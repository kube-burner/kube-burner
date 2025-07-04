# Instructions for Pushing to the Main Repository

You need to push your commits from your fork (`7908837174/kube-burner-kallal`) to the main upstream repository (`kube-burner/kube-burner`). Here's how to do it:

## 1. Verify the changes are ready

Your changes to `test/helpers.bash` and other files include:

- Added a directory-based locking mechanism in the service checker setup to prevent race conditions (more reliable than flock)
- Enhanced netcat verification with multiple fallback methods for BusyBox compatibility
- Fixed pod deletion and creation logic with better diagnostics
- Optimized resource requirements for the service checker pod
- Fixed shellcheck issues in test scripts
- Improved error handling with explicit failures instead of silent skipping
- Added timeout and stale lock cleanup for service checker lock
- Added unique test UUIDs for parallel test runs

## 2. Set up remotes and push to your fork

First, you need to push your changes to your fork. Run these commands in a terminal:

```bash
# Navigate to the repository
cd /workspaces/kube-burner

# Make sure git is configured
git config --global user.name "Your Name"
git config --global user.email "your.email@example.com"

# Set up remotes properly
git remote set-url origin https://github.com/7908837174/kube-burner-kallal.git
git remote add upstream https://github.com/kube-burner/kube-burner.git || git remote set-url upstream https://github.com/kube-burner/kube-burner.git

# Fetch latest from both remotes
git fetch origin
git fetch upstream

# Make sure you're on the main branch
git checkout main

# Push your changes to your fork
git push --force origin main
```
```

## 3. Create a Pull Request to the main repository

There are two ways to create a pull request from your fork to the main repository:

### Option 1: Using the GitHub web interface

1. Go to: https://github.com/kube-burner/kube-burner/compare/main...7908837174:kube-burner-kallal:main

2. Click "Create pull request"

3. Fill in the details:
   - Title: "Fix service checker pod setup and stabilize test infrastructure"
   - Description:
     ```
     - Add file locking mechanism to prevent race conditions
     - Enhance netcat verification with multiple fallback methods
     - Fix pod deletion and creation logic with better diagnostics
     - Optimize resource requirements for service checker pod
     - Improve error handling with explicit failures
     - Fix shellcheck issues in test scripts
     ```

4. Submit the PR

### Option 2: Using GitHub CLI

If you have the GitHub CLI installed and configured:

```bash
gh pr create --repo kube-burner/kube-burner \
  --head 7908837174:kube-burner-kallal:main \
  --base main \
  --title "Fix service checker pod setup and stabilize test infrastructure" \
  --body "- Add file locking mechanism to prevent race conditions
- Enhance netcat verification with multiple fallback methods
- Fix pod deletion and creation logic with better diagnostics
- Improve error handling with explicit failures
- Fix shellcheck issues in test scripts"
```

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
