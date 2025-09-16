# VS Code Remote Development Safety Guide

This setup provides comprehensive protection against file corruption and data loss during remote VS Code development.

## ✅ What's Been Configured

### 1. VS Code Settings (`.vscode/settings.json`)
- **Auto-save**: Files save every 2 seconds with error checking
- **Connection resilience**: Better SSH timeout and reconnection settings
- **Git integration**: Auto-fetch and smart commit features
- **File protection**: Confirmation dialogs for dangerous operations

### 2. Automatic Backup System
- **Auto-backup script**: `.vscode/auto-backup.sh`
- **VS Code tasks**: Accessible via `Ctrl+Shift+P` → "Tasks: Run Task"
- **Corruption detection**: Automatically detects and handles file corruption

### 3. Real-time File Monitoring
- **Safety monitor**: `.vscode/file-safety-monitor.sh`
- **Live corruption detection**: Monitors files as you edit
- **Automatic recovery**: Attempts to restore from git when corruption is detected

### 4. Enhanced SSH Configuration
- **Connection stability**: Keep-alive and multiplexing settings
- **Example config**: `.vscode/ssh-config-example`

## 🚀 How to Use

### Daily Development Workflow

1. **Start safety monitoring** (optional, for extra protection):
   ```bash
   ./.vscode/file-safety-monitor.sh install-deps  # First time only
   ./.vscode/file-safety-monitor.sh monitor       # Start monitoring
   ```

2. **Manual backup** (if needed):
   - Press `Ctrl+Shift+P`
   - Type "Tasks: Run Task"
   - Select "Auto Backup Now"

3. **Check for corruption**:
   - Press `Ctrl+Shift+P`
   - Type "Tasks: Run Task"  
   - Select "Check File Corruption"

4. **View safety status**:
   ```bash
   ./.vscode/file-safety-monitor.sh status
   ```

### Emergency Recovery

If you detect file corruption:

1. **Don't panic** - backups are automatic
2. **Check git history**: `git log --oneline`
3. **Restore from git**: `git restore <filename>`
4. **Use backup files**: Check `.vscode/backups/` directory
5. **Check log**: View `.vscode/safety.log` for details

### SSH Client Configuration

Add this to your **local machine's** `~/.ssh/config` file:

```ssh
# Copy settings from .vscode/ssh-config-example
Host your-remote-server
    HostName YOUR_SERVER_IP
    User bill
    ServerAliveInterval 60
    ServerAliveCountMax 3
    ControlMaster auto
    ControlPath ~/.ssh/control-%r@%h:%p
    ControlPersist 600
```

## 📊 Safety Features Active

- ✅ **Auto-save every 2 seconds** (prevents loss during connection drops)
- ✅ **Git auto-fetch** (keeps repository up-to-date)
- ✅ **Corruption detection** (finds duplicate lines, binary data, etc.)
- ✅ **Automatic backups** (timestamped copies in `.vscode/backups/`)
- ✅ **Connection resilience** (SSH keep-alive and multiplexing)
- ✅ **File monitoring** (real-time corruption detection with inotify)
- ✅ **Recovery assistance** (automatic git restore attempts)

## 🔧 Troubleshooting

### If you see file corruption:
1. Check `.vscode/safety.log` for details
2. Look for `.git-restore` files (automatic recovery attempts)
3. Check `.vscode/backups/` for timestamped backups
4. Use `git log` to see recent commits

### If connection is unstable:
1. Check your SSH configuration
2. Consider using connection multiplexing
3. Monitor network quality
4. Use the backup tasks more frequently

### If monitoring isn't working:
1. Install dependencies: `./.vscode/file-safety-monitor.sh install-deps`
2. Check if inotify-tools is installed: `which inotifywait`
3. Review log file: `cat .vscode/safety.log`

## 📝 Keyboard Shortcuts

- `Ctrl+Shift+P`, then type "Tasks" to access backup tasks
- `Ctrl+S` saves immediately (auto-save is also active)
- `Ctrl+Z` / `Ctrl+Y` for undo/redo (works with auto-save)

## 🎯 Best Practices

1. **Commit frequently** - Safety commits are automatic, but manual commits with good messages are better
2. **Monitor connection quality** - Use stable networks when possible
3. **Keep git remotes up-to-date** - Push to remote repositories regularly
4. **Check logs periodically** - Review `.vscode/safety.log` for issues
5. **Test recovery procedures** - Familiarize yourself with restoration process

---

**Remember**: These safety measures protect against corruption, but good development practices (regular commits, remote backups, code reviews) are still essential!