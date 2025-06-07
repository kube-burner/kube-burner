{{- define "env_func" -}}
{{- range $i := until $.n }}
{{- printf "- name: ENVVAR%d_%s\n  value: %s" (add $i 1) $.envName $.envVar | nindent $.indent }}
{{- end }}
{{- end }}
