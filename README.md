# Folder Backup Tool

A lightweight Windows desktop application for automated folder backups with intelligent change detection. Runs as a system tray application with configurable schedules and automatic cleanup. I built this to copy Steam dedicated server files to a Dropbox folder, since cloud saves usually aren't enabled with servers like that. (Palworld, 7 Days to Die, etc.)

## Features

- **Automated Backups**: Schedule regular backups with customizable intervals
- **Hash-Based Change Detection**: Skip backups when content hasn't changed, saving time and storage
- **System Tray Interface**: Unobtrusive desktop application with real-time status updates
- **Multiple Backup Configs**: Support for backing up multiple folders with different schedules
- **Automatic Cleanup**: Configurable retention policies to manage backup storage
- **Single Instance**: Prevents multiple copies from running simultaneously
- **Detailed Logging**: Per-backup logging with automatic log rotation

## Quick Start

1. **Download** the `backup-tool.exe` from the releases
2. **Run** the executable - it will create a default `config.json`
3. **Configure** your backup sources and destinations in `config.json`
4. **Restart** the application to load your configuration

The application will run in your system tray and begin automated backups according to your schedule.

## Configuration

The tool uses a `config.json` file for configuration:

```json
{
  "backups": [
    {
      "name": "My Important Folder",
      "source": "C:\\Source\\Folder",
      "destination": "D:\\Backups\\Destination",
      "schedule_minutes": 30,
      "rotation_count": 5,
      "enabled": true,
      "hash_check": true,
      "log_retention_days": 7
    }
  ]
}
```

### Configuration Options

| Option | Description | Default |
|--------|-------------|---------|
| `name` | Friendly name for the backup job | Required |
| `source` | Path to folder to backup | Required |
| `destination` | Where to store backup folders | Required |
| `schedule_minutes` | Backup interval in minutes | Required |
| `rotation_count` | Number of backup folders to keep | Required |
| `enabled` | Enable/disable this backup job | `true` |
| `hash_check` | Enable hash-based change detection | `true` |
| `log_retention_days` | Days to keep log files | `7` |

## How It Works

### Backup Process
1. **Hash Check** (if enabled): Calculate directory hash to detect changes
2. **Skip or Backup**: Skip if content unchanged, otherwise create timestamped backup
3. **Cleanup**: Remove old backups beyond retention count
4. **Status Update**: Update system tray with completion time

### Backup Naming
Backups are stored with timestamps: `DD-MM-YYYY_HH-MM-SS_SourceFolderName`

Example: `10-08-2025_14-30-15_MyFolder`

### Intelligent Scheduling
The scheduler considers both actual backups and skipped operations when determining the next backup time, ensuring consistent intervals regardless of content changes.

## System Tray Interface

- **Last backup**: Shows when the most recent backup completed
- **Next backup**: Countdown to next scheduled backup
- **[S] indicator**: Shows when last operation was skipped due to unchanged content
- **Exit**: Cleanly shutdown the application

## Logs

Logs are stored in the `logs/` directory:
- `system.log`: Application-level events (cleared on startup)
- `logs/[backup-name]/backup_DD-MM-YYYY.log`: Per-backup daily logs

## Requirements

- Windows (tested on Windows 10/11)
- Write access to source and destination folders
- Sufficient disk space for backups

## Building from Source

Requirements:
- Go 1.24+
- Windows (for icon embedding)

```bash
# Clone repository
git clone <repository-url>
cd folder-backup-tool

# Install rsrc for icon embedding
go install github.com/akavel/rsrc@latest

# Compile icon resource
rsrc -ico icon.ico -o rsrc.syso

# Build executable
go build -ldflags "-H=windowsgui -s -w" -o backup-tool.exe
```

## Advanced Usage

### Multiple Backup Jobs
Configure multiple backup jobs in the same `config.json`:

```json
{
  "backups": [
    {
      "name": "Documents",
      "source": "C:\\Users\\Username\\Documents",
      "destination": "D:\\Backups\\Documents",
      "schedule_minutes": 60,
      "rotation_count": 24
    },
    {
      "name": "Pictures",
      "source": "C:\\Users\\Username\\Pictures", 
      "destination": "D:\\Backups\\Pictures",
      "schedule_minutes": 1440,
      "rotation_count": 7
    }
  ]
}
```

### Disabling Hash Checking
Set `"hash_check": false` to disable change detection and always perform backups regardless of content changes.

### Log Retention
Adjust `log_retention_days` to control how long backup logs are kept. Set to higher values for systems requiring longer audit trails.

## Troubleshooting

### Application Won't Start
- Check if another instance is already running (look for system tray icon)
- Verify `config.json` is valid JSON
- Check `logs/system.log` for startup errors

### Backups Not Running  
- Verify source and destination paths exist and are accessible
- Check per-backup logs in `logs/[backup-name]/`
- Ensure sufficient disk space in destination

### "Another Instance Running" Message
- Close existing instance from system tray before starting new one
- If no tray icon visible, check Task Manager for `backup-tool.exe` process
- Delete `backup-tool.lock` file if process crashed

## License

Copyright (C) 2025. This software is provided as-is without warranty.

## Support

For issues, questions, or feature requests, please check the project documentation or create an issue in the repository.