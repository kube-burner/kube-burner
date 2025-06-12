# Templating Custom Resource Kinds

Starting from kube-burner v0.15.0, you can use Go template syntax to dynamically generate the `kind` field in Kubernetes resource definitions. This is particularly useful when you need to create multiple CRDs with different kinds in a parameterized way.

## Example Usage

Here's an example of a CRD template with a templated kind:

```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: testcrds{{.Iteration}}.testcrd.example.com
spec:
  group: testcrd.example.com
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                workload:
                  type: string
                iterations:
                  type: integer
  scope: Namespaced
  names:
    plural: testcrds{{.Iteration}}
    singular: testcrd{{.Iteration}}
    kind: TestCRD{{.Iteration}}
    shortNames:
    - tcrd{{.Iteration}}
```

And a corresponding CR that uses the templated kind:

```yaml
apiVersion: testcrd.example.com/v1
kind: TestCRD{{.Iteration}}
metadata:
  name: test{{.Replica}}
spec:
  workload: {{randAlpha 5}}
  iterations: {{.Iteration}}
```

## Ordering of Operations

When using templated kinds, kube-burner automatically:

1. Detects Custom Resource Definitions and ensures they are created first
2. Waits for the CRDs to be fully established before creating any associated Custom Resources
3. Properly handles template rendering to ensure that the `kind` field is correctly resolved

This ensures that resources with templated kinds are created in the correct order and with the proper dependencies.
