# Templating Custom Resource Kinds in Kube-Burner

This implementation allows the use of Go template variables in the `kind` field of Kubernetes resource definitions within kube-burner.

## Implementation Details

The fix for issue #862 involved several key changes:

1. **Pre-rendering templates before GVK identification**:
   - Modified `setupCreateJob` to render the template with sample data before determining the object kind
   - This ensures that templated `kind` fields are correctly resolved before resource mapping occurs

2. **CRD detection and ordering**:
   - Added an `isCRD` field to the `object` struct to identify Custom Resource Definitions
   - Split object creation into two phases: first CRDs, then non-CRDs
   - Added a synchronization mechanism to ensure CRDs are fully established before CRs are created

3. **Improved re-decoding with templated fields**:
   - Enhanced the re-decoding process in `replicaHandler` to preserve templated fields

## Testing

Two levels of testing were implemented:

1. **Minimal Test**: A focused test that only verifies template rendering works correctly
2. **Full Test**: Uses the modified kube-burner binary to create CRDs and CRs with templated kinds

Both tests confirm that:
- Templates with variables in the `kind` field render correctly
- CRDs are created with properly templated kinds
- CRs referencing those CRDs are created successfully
- The order of creation is correct (CRDs before CRs)

## Usage Example

```yaml
# CRD with templated kind
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: testcrds{{.Iteration}}.testcrd.example.com
spec:
  # ... CRD spec ...
  names:
    plural: testcrds{{.Iteration}}
    singular: testcrd{{.Iteration}}
    kind: TestCRD{{.Iteration}}
```

```yaml
# CR referencing templated kind
apiVersion: testcrd.example.com/v1
kind: TestCRD{{.Iteration}}
metadata:
  name: test{{.Replica}}
spec:
  workload: {{randAlpha 5}}
  iterations: {{.Iteration}}
```
