package workloads

var MetricsProfileMap = map[string]string{
	"cluster-density-ms":             "metrics-aggregated.yml",
	"cluster-density-v2":             "metrics-aggregated.yml",
	"cluster-density":                "metrics-aggregated.yml",
	"crd-scale":                      "metrics-aggregated.yml",
	"node-density":                   "metrics.yml",
	"node-density-heavy":             "metrics.yml",
	"node-density-cni":               "metrics.yml",
	"networkpolicy-multitenant":      "metrics.yml",
	"networkpolicy-matchlabels":      "metrics.yml",
	"networkpolicy-matchexpressions": "metrics.yml",
	"pvc-density":                    "metrics.yml",
}
