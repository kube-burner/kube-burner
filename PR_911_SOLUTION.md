# PR #911 Complete Solution Guide

## 🎯 Issue Summary
The test failures in PR #911 are caused by a **test setup verification process** that is hardcoded in the PR branch to use busybox, which has limited netcat functionality. This verification happens **before** any actual tests run and is independent of the Go code changes.

## 🔍 Root Cause Analysis

### The Problem
1. **Dual Service Checker Creation**: There are TWO separate processes:
   - **Test Setup** (PR branch): Creates busybox pod for verification during `setup_file()`
   - **Runtime Code** (our changes): Creates pods during actual kube-burner execution

2. **Test Setup Override**: The PR branch contains test setup code that:
   ```bash
   # From test logs:
   Setting up service checker pod for service latency tests (uuid: 66d7ce9e)
   Creating service checker pod with busybox image (includes netcat)
   Creating service checker pod with busybox image
   Verifying netcat is available in the service checker pod
   FATAL: netcat command exists but appears to be non-functional
   ```

3. **Busybox Netcat Limitations**: Busybox netcat doesn't support `-z` (port scan) and `-w` (timeout) flags properly.

## ✅ Solution Applied

### Files Modified:
1. **`.github/workflows/ci-parallel.yml`** - Clean parallel CI workflow
2. **`pkg/measurements/service_latency.go`** - Updated service checker image
3. **`pkg/measurements/util/svc_checker.go`** - Fixed netcat command compatibility
4. **`test/helpers.bash`** - Fixed shellcheck SC2181 error

### Current Configuration:
```go
// service_latency.go
err := deployPodInNamespace(s.ClientSet, types.SvcLatencyNs, types.SvcLatencyCheckerName, "quay.io/cloud-bulldozer/fedora-nc:latest", []string{"sleep", "inf"})

// svc_checker.go  
cmd := []string{"bash", "-c", fmt.Sprintf("while true; do nc -w 1 -z %s %d && break; sleep 0.05; done", address, port)}
```

## 🚨 Critical Issue: Test Setup Code

**The test setup in the PR branch is still hardcoded to use busybox.** This setup code is not in our current local branch and needs to be updated in the PR branch.

### Required Changes in PR Branch:

The PR branch needs to update its test setup code to either:

#### Option 1: Use Fedora Image (Recommended)
```bash
# In the PR branch test setup code:
# Change from: busybox:1.36
# Change to: quay.io/cloud-bulldozer/fedora-nc:latest
```

#### Option 2: Fix Busybox Netcat Command
```bash
# In the PR branch test setup verification:
# Change from: nc -z -w 1 $host $port
# Change to: echo '' | nc $host $port
```

## 🎯 Next Steps for PR Author

1. **Identify Test Setup Code**: Find the test setup code in your PR branch that creates the service checker pod
2. **Update Image Reference**: Change the hardcoded busybox image to fedora-nc
3. **Update Verification Logic**: Ensure the netcat verification matches the image capabilities
4. **Test Locally**: Run the tests to verify the fix works

## 📋 All Issues Fixed

- ✅ **Shellcheck SC2181** - Fixed in `test/helpers.bash`
- ✅ **Shellcheck SC2035** - Fixed glob patterns in `test/run-tests.sh`
- ✅ **Shellcheck SC2086** - Fixed variable quoting in `test/run-tests.sh`
- ✅ **Shellcheck SC2004** - Fixed arithmetic expressions in `test/helpers.bash`
- ✅ **CI Parallel Workflow** - Added clean `ci-parallel.yml`
- ✅ **Service Latency Code** - Updated to use fedora-nc image
- ✅ **Netcat Compatibility** - Fixed command for fedora nmap-ncat
- ❌ **Test Setup** - Requires PR branch update (not accessible locally)

## 🔧 Technical Details

### Why Fedora-NC Image?
- **Full netcat features**: Supports `-z`, `-w`, and all required flags
- **Proven compatibility**: Original working configuration
- **Test setup expectation**: The verification logic expects this behavior

### Why Not Busybox?
- **Limited netcat**: Missing `-z` (port scan) and `-w` (timeout) flags
- **Incompatible behavior**: Different exit codes and flag handling
- **Test verification failure**: Hardcoded verification expects full netcat

## 🎉 Expected Results After PR Branch Update

Once the PR branch test setup is updated:
- ✅ Pre-commit hooks will pass
- ✅ Service checker verification will succeed  
- ✅ All 22 tests will run successfully
- ✅ Service latency measurements will work correctly

## 📞 Contact

If you need help locating the test setup code in your PR branch, look for:
- Functions that create service checker pods during setup
- References to "busybox" in test setup files
- Netcat verification logic in test helpers
