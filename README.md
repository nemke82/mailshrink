<p align="center">
  <strong>MailShrink</strong><br>
  Safely reclaim disk space on Dovecot Maildir servers.
</p>

<p align="center">
  <a href="https://github.com/nemke82/mailshrink/releases">Releases</a> •
  <a href="https://nemke82.github.io/mailshrink/">Website</a> •
  <a href="SECURITY.md">Security</a>
</p>

---

> MailShrink does not simply run `gzip` over your Maildir. It understands Dovecot Maildir metadata, preserves `INTERNALDATE`, validates `S=` metadata, uses mailbox locking, detects existing compression, performs atomic replacement, and verifies messages before removing the uncompressed copy.

## Demo

[![asciicast](https://asciinema.org/a/1Gm7cHYh8FfGNXTA.svg)](https://asciinema.org/a/1Gm7cHYh8FfGNXTA)


## Install

```bash
curl -sL https://github.com/nemke82/mailshrink/releases/latest/download/mailshrink-linux-amd64 \
  -o /usr/local/bin/mailshrink && chmod +x /usr/local/bin/mailshrink
```

ARM64:
```bash
curl -sL https://github.com/nemke82/mailshrink/releases/latest/download/mailshrink-linux-arm64 \
  -o /usr/local/bin/mailshrink && chmod +x /usr/local/bin/mailshrink
```

Works on any Linux server — Ubuntu, Debian, AlmaLinux, Rocky, RHEL, or any distro running Dovecot with Maildir storage. Single static binary, no dependencies.

### Prerequisites

Dovecot with the zlib plugin. Most hosting panels already have it — run `mailshrink check` to verify.

```bash
# Ubuntu / Debian
apt install dovecot-core

# AlmaLinux / Rocky / RHEL
dnf install dovecot
```

## Quick Start

**1. Verify Dovecot is ready:**

```bash
mailshrink check
```

**2. See how much space you can reclaim:**

```bash
mailshrink analyze
```

**3. Get a detailed plan:**

```bash
mailshrink plan example.com
```

**4. Compress (dry-run first, then apply):**

```bash
# See what would happen (no files touched):
mailshrink compress --domain example.com --folder Sent --before 2025-01-01

# Actually compress:
mailshrink compress --domain example.com --folder Sent --before 2025-01-01 --apply
```

## How It Works

MailShrink gzip-compresses old email files in place. Dovecot's [zlib plugin](https://doc.dovecot.org/settings/plugin/zlib-plugin/) transparently decompresses them on read — users see no difference.

**Safety first:**
- **Dry-run by default** — `compress` requires `--apply` to modify files
- **Dovecot check** — verifies zlib is enabled before compressing
- **Atomic operations** — original file untouched until compression succeeds
- **Metadata preserved** — mtime (IMAP date), ownership, permissions all restored
- **Maildir locking** — prevents conflicts with Dovecot
- **No deletion** — MailShrink never deletes email

**Measure, don't guess** — instead of assuming "gzip saves ~20%", MailShrink samples your actual messages, compresses them in memory, and reports the real measured ratio.

## Commands

| Command | Description |
|---------|-------------|
| `check` | Verify Dovecot + zlib plugin readiness |
| `analyze` | Scan mailboxes, sample messages, estimate savings |
| `plan` | Detailed per-account/folder breakdown |
| `compress` | Compress messages (dry-run by default, `--apply` to execute) |
| `version` | Print version info |

### Key Flags

| Flag | Available On | Description |
|------|-------------|-------------|
| `--path` | all | Base scan path (default: `/home`) |
| `--domain` | compress | Target domain |
| `--account` | compress | Target account (`user@domain`) |
| `--folder` | analyze, plan, compress | Target folder (Sent, INBOX, etc.) |
| `--before` | compress | Only messages before date (`YYYY-MM-DD`) |
| `--older-than` | analyze, plan, compress | Age filter (`2y`, `6m`, `90d`) |
| `--apply` | compress | **Required** to actually modify files |
| `--json` | all | Machine-readable output |
| `--provider` | all | Force panel type (`cpanel`, `directadmin`, `generic`) |

## Supported Servers

MailShrink auto-detects your server layout:

| Panel | Mail Path | Status |
|-------|-----------|--------|
| **cPanel / WHM** | `/home/user/mail/domain/account/` | ✅ |
| **DirectAdmin** | `/home/user/imap/domain/account/Maildir/` | ✅ |
| **Custom Dovecot** | Any Maildir with `cur/new/tmp` | ✅ |
| **Plesk** | `/var/qmail/mailnames/domain/account/Maildir/` | 🔜 |

## Origin

Based on the article [How I Saved Gigabytes of Disk Space by Compressing Old Dovecot Maildir Emails on cPanel](https://nemanja.io/how-i-saved-gigabytes-of-disk-space-by-compressing-old-dovecot-maildir-emails-on-cpanel/), which helped reclaim free disk space on production servers. MailShrink turns that manual process into a safe, automated tool.

## License

[MIT](LICENSE)
