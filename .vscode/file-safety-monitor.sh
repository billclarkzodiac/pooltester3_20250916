#!/bin/bash

# Advanced File Monitoring and Safety Script for Remote VS Code Development
# This script monitors for file corruption, creates automatic backups, and handles recovery

PROJECT_DIR="/home/bill/projects/pooltester3"
BACKUP_DIR="$PROJECT_DIR/.vscode/backups"
LOG_FILE="$PROJECT_DIR/.vscode/safety.log"

# Create backup directory
mkdir -p "$BACKUP_DIR"

# Logging function
log_message() {
    local timestamp=$(date "+%Y-%m-%d %H:%M:%S")
    echo "[$timestamp] $1" | tee -a "$LOG_FILE"
}

# Function to create timestamped backup
create_backup() {
    local file="$1"
    local backup_name="$(basename "$file").$(date +%s).backup"
    cp "$file" "$BACKUP_DIR/$backup_name"
    log_message "Created backup: $backup_name"
}

# Function to detect file corruption patterns
detect_corruption() {
    local file="$1"
    local corruption_found=false
    
    if [[ ! -f "$file" ]]; then
        return 1
    fi
    
    # Check for duplicate consecutive lines (common SSH corruption)
    if grep -Pzo '(.+)\n\1' "$file" >/dev/null 2>&1; then
        log_message "WARNING: Duplicate lines detected in $file"
        corruption_found=true
    fi
    
    # Check for binary data in text files
    if file "$file" | grep -q "text" && grep -qP '[^\x20-\x7E\n\r\t]' "$file"; then
        log_message "WARNING: Binary data detected in text file $file"
        corruption_found=true
    fi
    
    # Check for truncated files (files that end abruptly)
    if [[ "$file" == *.go ]] && ! grep -q "^}" "$file"; then
        log_message "WARNING: Go file $file may be truncated (no closing brace)"
        corruption_found=true
    fi
    
    # Check for malformed JSON
    if [[ "$file" == *.json ]] && ! python3 -m json.tool "$file" >/dev/null 2>&1; then
        log_message "WARNING: Malformed JSON detected in $file"
        corruption_found=true
    fi
    
    if [[ "$corruption_found" == "true" ]]; then
        # Create emergency backup of corrupted file
        create_backup "$file"
        
        # Try to restore from git
        restore_from_git "$file"
        
        return 0
    fi
    
    return 1
}

# Function to restore file from git
restore_from_git() {
    local file="$1"
    local relative_path="${file#$PROJECT_DIR/}"
    
    cd "$PROJECT_DIR" || return 1
    
    if git show HEAD:"$relative_path" > "$file.git-restore" 2>/dev/null; then
        log_message "Restored clean version of $file from git (saved as $file.git-restore)"
        
        # Ask user if they want to replace the corrupted file
        log_message "To replace corrupted file, run: mv '$file.git-restore' '$file'"
    else
        log_message "Could not restore $file from git"
    fi
}

# Function to monitor files using inotify
start_file_monitor() {
    log_message "Starting file monitor for $PROJECT_DIR"
    
    # Monitor for file modifications
    inotifywait -m -r -e modify,create,delete,move "$PROJECT_DIR" \
        --exclude '\.git|\.vscode/backups|node_modules' \
        --format '%w%f %e %T' --timefmt '%Y-%m-%d %H:%M:%S' \
    | while read FILE EVENT TIME; do
        
        # Skip certain files and directories
        if [[ "$FILE" == */.git/* ]] || [[ "$FILE" == */.vscode/backups/* ]]; then
            continue
        fi
        
        log_message "File event: $FILE ($EVENT) at $TIME"
        
        # Check for corruption on modify events
        if [[ "$EVENT" == "MODIFY" ]] && [[ -f "$FILE" ]]; then
            if detect_corruption "$FILE"; then
                log_message "CORRUPTION DETECTED AND HANDLED: $FILE"
            fi
        fi
        
        # Create backup on important file changes
        if [[ "$EVENT" == "MODIFY" ]] && [[ "$FILE" =~ \.(go|json|md|sh)$ ]]; then
            create_backup "$FILE"
        fi
        
    done
}

# Function to run periodic checks
run_periodic_checks() {
    log_message "Running periodic safety checks"
    
    # Check all important files for corruption
    find "$PROJECT_DIR" -name "*.go" -o -name "*.json" -o -name "*.md" | while read file; do
        detect_corruption "$file"
    done
    
    # Clean old backups (keep last 50)
    find "$BACKUP_DIR" -type f -name "*.backup" | sort -r | tail -n +51 | xargs rm -f 2>/dev/null
    
    # Auto-commit if there are changes
    cd "$PROJECT_DIR" || return 1
    if ! git diff --quiet || ! git diff --cached --quiet; then
        git add -A
        git commit -m "AUTO-SAFETY: Periodic backup commit $(date '+%Y-%m-%d %H:%M:%S')" 2>/dev/null
        log_message "Created periodic safety commit"
    fi
}

# Function to show status
show_status() {
    echo "=== Remote Development Safety Status ==="
    echo "Project: $PROJECT_DIR"
    echo "Backups: $(ls -1 "$BACKUP_DIR" 2>/dev/null | wc -l) files"
    echo "Log entries: $(wc -l < "$LOG_FILE" 2>/dev/null || echo 0)"
    echo ""
    echo "Recent log entries:"
    tail -n 10 "$LOG_FILE" 2>/dev/null || echo "No log entries yet"
}

# Main execution
case "${1:-monitor}" in
    "monitor")
        if command -v inotifywait >/dev/null 2>&1; then
            start_file_monitor
        else
            log_message "ERROR: inotifywait not found. Install inotify-tools package."
            log_message "On Ubuntu/Debian: sudo apt-get install inotify-tools"
            exit 1
        fi
        ;;
    "check")
        run_periodic_checks
        ;;
    "status")
        show_status
        ;;
    "install-deps")
        log_message "Installing dependencies..."
        if command -v apt-get >/dev/null 2>&1; then
            sudo apt-get update && sudo apt-get install -y inotify-tools
        elif command -v yum >/dev/null 2>&1; then
            sudo yum install -y inotify-tools
        else
            log_message "Please install inotify-tools manually for your system"
        fi
        ;;
    *)
        echo "Usage: $0 [monitor|check|status|install-deps]"
        echo ""
        echo "Commands:"
        echo "  monitor      - Start real-time file monitoring (requires inotify-tools)"
        echo "  check        - Run periodic corruption checks and cleanup"
        echo "  status       - Show current safety status"
        echo "  install-deps - Install required dependencies"
        exit 1
        ;;
esac