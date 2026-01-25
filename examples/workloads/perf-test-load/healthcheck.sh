#!/bin/sh
oc get co --no-headers | awk '$3!="True" || $4!="False" || $5!="False" {exit 1}'
echo Hello
