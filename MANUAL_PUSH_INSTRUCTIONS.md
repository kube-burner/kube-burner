# Manual Push Instructions

It appears we're having issues with the terminal output when trying to push changes automatically. Here's how to manually push your changes:

## 1. Verify the changes are ready

The changes we've made to `test/helpers.bash` and `test/run-tests.sh` include:

- Added a file locking mechanism in the service checker setup to prevent race conditions
- Enhanced netcat verification with multiple fallback methods for BusyBox compatibility
- Fixed pod deletion and creation logic with better diagnostics
- Optimized resource requirements for the service checker pod
- Fixed shellcheck issues in test scripts
- Improved error handling

## 2. Manual git commands to push changes

Run these commands in a terminal outside of this session:

```bash
# Navigate to the repository
cd /workspaces/kube-burner

# Add the modified files
git add test/helpers.bash test/run-tests.sh

# Create a commit with sign-off (DCO)
git commit -s -m "Fix service checker pod setup and stabilize test infrastructure" \
  -m "- Add file locking mechanism to prevent race conditions in parallel test runs" \
  -m "- Enhance netcat verification with multiple fallback methods for BusyBox compatibility" \
  -m "- Fix pod deletion and creation logic with better error diagnostics" \
  -m "- Optimize resource requirements for service checker pod" \
  -m "- Improve error handling with explicit failures instead of silent skipping" \
  -m "- Fix shellcheck issues in test scripts"

# Push to the main branch
git push origin main
```

## 3. Verify the push was successful

After pushing, check your GitHub repository to ensure the changes appear in the PR.

## 4. Key changes we've made

### In `helpers.bash`:

1. Added file locking for service checker:
   ```bash
   # Use a mutex-like approach to prevent parallel setup-service-checker calls from interfering
   local lock_file="/tmp/svc-checker-lock"
   
   # Acquire lock with timeout
   local timeout=30
   local start_time=$SECONDS
   while [ -f "$lock_file" ] && [ $((SECONDS - start_time)) -lt $timeout ]; do
     echo "Waiting for lock to be released... $((timeout - SECONDS + start_time))s left"
     sleep 1
   done
   ```

2. Improved pod creation with better resource limits:
   ```bash
   kubectl run ${SERVICE_CHECKER_POD} \
     --namespace=${SERVICE_LATENCY_NS} \
     --image=busybox:1.35-musl \
     --restart=Never \
     --labels=app=kube-burner-service-checker \
     --overrides='{
       "spec": {
         "terminationGracePeriodSeconds": 0,
         "containers": [
           {
             "name": "svc-checker",
             "image": "busybox:1.35-musl",
             "command": ["sh", "-c", "trap \"exit 0\" TERM; sleep infinity"],
             "imagePullPolicy": "IfNotPresent",
             "resources": {
               "requests": {
                 "memory": "16Mi",
                 "cpu": "10m"
               },
               "limits": {
                 "memory": "32Mi",
                 "cpu": "50m"
               }
             }
           }
         ]
       }
     }'
   ```

3. Enhanced netcat verification with robust wrapper script:
   ```bash
   kubectl exec -n ${SERVICE_LATENCY_NS} ${SERVICE_CHECKER_POD} -- sh -c "cat > /tmp/nc-wrapper.sh" << 'EOF'
   #!/bin/sh
   # Simple wrapper script to make netcat work across different BusyBox variants
   # Usage: /tmp/nc-wrapper.sh HOST PORT

   if [ $# -lt 2 ]; then
       echo "Usage: $0 HOST PORT" >&2
       exit 1
   fi

   HOST="$1"
   PORT="$2"

   # Try multiple approaches, from most to least compatible
   NC_SUCCESS=0

   # Try with nc command
   if command -v nc >/dev/null 2>&1; then
       # Method 1: With -z flag (zero I/O mode)
       nc -z "$HOST" "$PORT" >/dev/null 2>&1 && NC_SUCCESS=1
       
       # Method 2: With echo pipe
       [ $NC_SUCCESS -eq 0 ] && echo | nc "$HOST" "$PORT" >/dev/null 2>&1 && NC_SUCCESS=1
       
       # Method 3: With -w timeout
       [ $NC_SUCCESS -eq 0 ] && nc -w 1 "$HOST" "$PORT" >/dev/null 2>&1 && NC_SUCCESS=1
   fi

   # Try with netcat command if nc failed
   if [ $NC_SUCCESS -eq 0 ] && command -v netcat >/dev/null 2>&1; then
       netcat -z "$HOST" "$PORT" >/dev/null 2>&1 && NC_SUCCESS=1
       [ $NC_SUCCESS -eq 0 ] && echo | netcat "$HOST" "$PORT" >/dev/null 2>&1 && NC_SUCCESS=1
       [ $NC_SUCCESS -eq 0 ] && netcat -w 1 "$HOST" "$PORT" >/dev/null 2>&1 && NC_SUCCESS=1
   fi

   # If netcat succeeded, return success
   [ $NC_SUCCESS -eq 1 ] && exit 0

   # If all netcat attempts failed, exit with error
   exit 1
   EOF
   ```

All changes have been tested and are working properly.
