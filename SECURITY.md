# Security Policy

## Overview

MailShrink operates directly on production email storage. Security and data integrity are the project's highest priorities.

## Privilege Requirements

- MailShrink must run as **root** or as the user who owns the mail files
- The tool never opens network connections and never transmits data
- All operations are local to the filesystem

## Data Safety Guarantees

1. **Atomic operations**: Compression writes to a temporary file first; the original is only replaced after successful compression via atomic `rename(2)`
2. **No data deletion**: MailShrink never deletes email messages — it only compresses them
3. **Metadata preservation**: File modification time (mtime = IMAP INTERNALDATE), ownership (UID/GID), and permissions are preserved
4. **Maildir locking**: Advisory file locks prevent concurrent access by Dovecot during compression
5. **Dry-run by default**: The `compress` command requires explicit `--apply` to modify any files
6. **Skip-on-error**: If any individual file fails to compress, it is skipped and the original remains untouched

## Supported Environments

MailShrink is designed for Linux servers running Dovecot with the zlib plugin enabled. It is tested on:

- cPanel/WHM servers
- DirectAdmin servers
- Custom Dovecot + Exim/Postfix setups

## Reporting a Vulnerability

If you discover a security vulnerability, please report it responsibly:

1. **Do not** open a public GitHub issue
2. Email: **security@nemanja.io** (or use GitHub's private vulnerability reporting)
3. Include:
   - A description of the vulnerability
   - Steps to reproduce
   - Potential impact
   - Suggested fix (if any)

We will acknowledge receipt within 48 hours and aim to provide a fix within 7 days for critical issues.

## Best Practices

Before using MailShrink on production data:

1. **Back up your mail storage** before the first run
2. **Test on a staging server** or a small subset of mailboxes first
3. **Verify Dovecot's zlib plugin** is enabled and configured
4. **Run `mailshrink analyze`** first to understand the scope
5. **Use `mailshrink compress`** without `--apply` to see what would happen
6. **Start small** — compress one account's Sent folder before doing a full run
