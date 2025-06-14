# Summary of Implementation for Issue #862

## Problem
When applying templates with variables in the `kind` field (e.g., `TestCRD{{.Iteration}}`), kube-burner fails because templating isn't applied properly before identifying the resource type.

## Solution
The fix involves several key changes:

1. **Pre-rendering templates before GVK identification**: 
   - Modified `setupCreateJob` to render the template with sample data before determining the object kind.
   - This ensures that templated `kind` fields are correctly resolved before the resource mapping occurs.

2. **CRD detection and ordering**:
   - Added an `isCRD` field to the `object` struct to identify Custom Resource Definitions.
   - Split object creation into two phases: first CRDs, then non-CRDs.
   - Added a synchronization mechanism to ensure CRDs are fully established before CRs are created.

3. **Improved re-decoding with templated fields**:
   - Added a comment in `replicaHandler` to clarify the re-decoding process that preserves templated fields.

## Implementation Details

### Main changes:

1. **In setupCreateJob**:
   - Parse template and identify the GVK with the templated fields rendered
   - Mark objects as CRDs when applicable

2. **In RunCreateJob**:
   - Separate objects into CRDs and non-CRDs
   - Process CRDs first, then wait for them to be established
   - Process non-CRDs after CRDs are ready

3. **In createRequest**:
   - Added logic to wait for CRDs to be established before proceeding

### Testing
The fix was tested with custom templates that include templated kind fields to verify:
1. CRDs are created with properly templated kinds
2. CRs referencing those CRDs are created successfully
3. The order of creation is correct (CRDs before CRs)

## Documentation
Added new documentation file explaining how to use templated kind fields in Kubernetes resource definitions.
