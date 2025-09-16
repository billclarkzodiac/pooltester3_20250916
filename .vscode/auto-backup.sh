#!/bin/bash

# Git Auto-Backup Script for Remote Development Safety
# This script creates automatic commits to protect against file corruption

PROJECT_DIR="/home/bill/projects/pooltester3"
BACKUP_BRANCH="auto-backup-$(date +%Y%m%d)"

cd "$PROJECT_DIR" || exit 1

# Function to create auto-backup
create_auto_backup() {
    local timestamp=$(date "+%Y-%m-%d %H:%M:%S")
    
    # Check if there are any changes
    if ! git diff --quiet || ! git diff --cached --quiet; then
        echo "[$timestamp] Creating auto-backup..."
        
        # Add all changes
        git add -A
        
        # Create backup commit
        git commit -m "AUTO-BACKUP: $timestamp - Remote development safety checkpoint"
        
        echo "[$timestamp] Auto-backup created successfully"
        
        # Optional: Push to remote if configured
        if git remote get-url origin >/dev/null 2>&1; then
            git push origin HEAD 2>/dev/null || echo "[$timestamp] Warning: Could not push to remote"
        fi
    else
        echo "[$timestamp] No changes to backup"
    fi
}

# Function to check for file corruption
check_file_corruption() {
    local timestamp=$(date "+%Y-%m-%d %H:%M:%S")
    
    # Look for common corruption patterns
    for file in *.go *.json *.md; do
        if [[ -f "$file" ]]; then
            # Check for duplicate lines (common corruption pattern)
            if grep -Pzo '(.+)\n\1' "$file" >/dev/null 2>&1; then
                echo "[$timestamp] WARNING: Possible corruption detected in $file (duplicate lines)"
                
                # Create emergency backup
                cp "$file" "$file.corrupted.$(date +%s)"
                
                # Try to restore from git if possible
                if git show HEAD:"$file" > "$file.restored" 2>/dev/null; then
                    echo "[$timestamp] Restored clean version of $file from git"
                fi
            fi
        fi
    done
}

# Main execution
case "${1:-backup}" in
    "backup")
        create_auto_backup
        ;;
    "check")
        check_file_corruption
        ;;
    "both")
        check_file_corruption
        create_auto_backup
        ;;
    *)
        echo "Usage: $0 [backup|check|both]"
        exit 1
        ;;
esac