// Package estimator provides sampling-based compression ratio estimation.
// Instead of guessing that "gzip normally saves 20%", MailShrink measures
// the actual expected compression ratio by sampling real messages.
package estimator

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"math/rand"
	"os"
	"sort"

	"github.com/nemke82/mailshrink/internal/maildir"
)

// EstimateResult contains the measured compression statistics.
type EstimateResult struct {
	// SampleCount is the number of messages actually sampled.
	SampleCount int

	// SampleOriginalSize is the total original size of sampled messages.
	SampleOriginalSize int64

	// SampleCompressedSize is the total compressed size of sampled messages.
	SampleCompressedSize int64

	// MeasuredRatio is the measured compression reduction ratio (0.0 to 1.0).
	// For example, 0.2746 means 27.46% reduction.
	MeasuredRatio float64

	// EstimatedSavings is the projected total savings extrapolated to all
	// uncompressed messages in the target set.
	EstimatedSavings int64

	// TotalUncompressedSize is the total size of all uncompressed messages
	// in the target set (not just the sample).
	TotalUncompressedSize int64

	// TotalUncompressedCount is the total number of uncompressed messages.
	TotalUncompressedCount int
}

// Options controls the estimation behavior.
type Options struct {
	// SampleSize is the number of messages to sample. Default: 100.
	SampleSize int

	// CompressionLevel is the gzip compression level (1-9). Default: 6.
	CompressionLevel int

	// Seed for random sampling reproducibility. Zero means random.
	Seed int64
}

// DefaultOptions returns the default estimation options.
func DefaultOptions() Options {
	return Options{
		SampleSize:       100,
		CompressionLevel: 6,
	}
}

// Estimate samples a subset of messages, compresses them in memory, and
// calculates the actual expected compression ratio. This is much more
// accurate than assuming a fixed ratio.
func Estimate(messages []*maildir.Message, opts Options) (*EstimateResult, error) {
	if opts.SampleSize <= 0 {
		opts.SampleSize = 100
	}
	if opts.CompressionLevel <= 0 || opts.CompressionLevel > 9 {
		opts.CompressionLevel = 6
	}

	// Filter to uncompressed messages only.
	var uncompressed []*maildir.Message
	var totalUncompressedSize int64
	for _, msg := range messages {
		if !msg.IsCompressed && msg.PhysicalSize > 0 {
			uncompressed = append(uncompressed, msg)
			totalUncompressedSize += msg.PhysicalSize
		}
	}

	if len(uncompressed) == 0 {
		return &EstimateResult{}, nil
	}

	// Select sample — weighted toward larger files for better estimates.
	sample := selectSample(uncompressed, opts.SampleSize, opts.Seed)

	// Compress each sampled message in memory.
	var totalOriginal, totalCompressed int64
	for _, msg := range sample {
		original, compressed, err := compressInMemory(msg.Path, opts.CompressionLevel)
		if err != nil {
			// Skip files we can't read — don't fail the estimation.
			continue
		}
		totalOriginal += original
		totalCompressed += compressed
	}

	if totalOriginal == 0 {
		return &EstimateResult{
			SampleCount:            len(sample),
			TotalUncompressedSize:  totalUncompressedSize,
			TotalUncompressedCount: len(uncompressed),
		}, nil
	}

	ratio := 1.0 - float64(totalCompressed)/float64(totalOriginal)
	estimatedSavings := int64(float64(totalUncompressedSize) * ratio)

	return &EstimateResult{
		SampleCount:            len(sample),
		SampleOriginalSize:     totalOriginal,
		SampleCompressedSize:   totalCompressed,
		MeasuredRatio:          ratio,
		EstimatedSavings:       estimatedSavings,
		TotalUncompressedSize:  totalUncompressedSize,
		TotalUncompressedCount: len(uncompressed),
	}, nil
}

// selectSample picks a representative sample of messages, weighted toward
// larger files. This gives more accurate byte-level estimates.
func selectSample(messages []*maildir.Message, sampleSize int, seed int64) []*maildir.Message {
	if len(messages) <= sampleSize {
		return messages
	}

	// Sort by size descending.
	sorted := make([]*maildir.Message, len(messages))
	copy(sorted, messages)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].PhysicalSize > sorted[j].PhysicalSize
	})

	// Take the top 20% by size (these are the most impactful).
	topCount := sampleSize / 5
	if topCount < 1 {
		topCount = 1
	}
	if topCount > len(sorted) {
		topCount = len(sorted)
	}

	sample := make([]*maildir.Message, 0, sampleSize)
	sample = append(sample, sorted[:topCount]...)

	// Fill remaining slots with random selection from the rest.
	remaining := sorted[topCount:]
	if len(remaining) == 0 {
		return sample
	}

	rng := rand.New(rand.NewSource(seed))
	if seed == 0 {
		rng = rand.New(rand.NewSource(rand.Int63()))
	}

	randomCount := sampleSize - topCount
	if randomCount > len(remaining) {
		randomCount = len(remaining)
	}

	// Fisher-Yates shuffle on the remaining, take first randomCount.
	for i := len(remaining) - 1; i > 0; i-- {
		j := rng.Intn(i + 1)
		remaining[i], remaining[j] = remaining[j], remaining[i]
	}
	sample = append(sample, remaining[:randomCount]...)

	return sample
}

// compressInMemory reads a file and compresses it to a buffer, returning
// the original and compressed sizes. No temp files are created.
func compressInMemory(path string, level int) (original int64, compressed int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return 0, 0, fmt.Errorf("stat %s: %w", path, err)
	}
	original = info.Size()

	var buf bytes.Buffer
	gw, err := gzip.NewWriterLevel(&buf, level)
	if err != nil {
		return 0, 0, fmt.Errorf("create gzip writer: %w", err)
	}

	if _, err := io.Copy(gw, f); err != nil {
		gw.Close()
		return 0, 0, fmt.Errorf("compress %s: %w", path, err)
	}

	if err := gw.Close(); err != nil {
		return 0, 0, fmt.Errorf("finalize gzip %s: %w", path, err)
	}

	compressed = int64(buf.Len())
	return original, compressed, nil
}
