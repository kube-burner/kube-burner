#!/bin/bash
# Simple test hook that logs execution details

set -e

HOOK_LOG_DIR="/tmp/kube-burner-hooks"
mkdir -p "$HOOK_LOG_DIR"

TIMESTAMP=$(date '+%Y-%m-%d %H:%M:%S')
HOOK_PHASE="${1:-unknown}"
LOG_FILE="$HOOK_LOG_DIR/hook-${HOOK_PHASE}.log"

echo "========================================" | tee -a "$LOG_FILE"
echo "Hook Execution: $HOOK_PHASE" | tee -a "$LOG_FILE"
echo "Timestamp: $TIMESTAMP" | tee -a "$LOG_FILE"
echo "PID: $$" | tee -a "$LOG_FILE"
echo "Environment:" | tee -a "$LOG_FILE"
env | grep -E "KUBE_BURNER|JOB_|UUID" | tee -a "$LOG_FILE" || echo "No kube-burner env vars" | tee -a "$LOG_FILE"
echo "========================================" | tee -a "$LOG_FILE"

# Simulate some work
sleep 1

echo "Hook $HOOK_PHASE completed successfully" | tee -a "$LOG_FILE"
exit 0
