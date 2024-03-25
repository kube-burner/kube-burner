import sys
import json
from jsonschema import Draft7Validator
from datetime import datetime

validators = {
    "podLatencyMeasurement": {
        "type": "array",
        "items": {
            "type": "object",
            "properties": {
                "timestamp": {"type": "string", "format": "date-time"},
                "schedulingLatency": {"type": "integer"},
                "initializedLatency": {"type": "integer"},
                "containersReadyLatency": {"type": "integer"},
                "podReadyLatency": {"type": "integer"},
                "metricName": {"type": "string"},
                "jobName": {"type": "string"},
                "uuid": {"type": "string"},
                "namespace": {"type": "string"},
                "podName": {"type": "string"},
                "nodeName": {"type": "string"},
            },
            "required": [
                "timestamp",
                "schedulingLatency",
                "initializedLatency",
                "containersReadyLatency",
                "podReadyLatency",
                "metricName",
                "jobName",
                "uuid",
                "namespace",
                "podName",
                "nodeName",
            ],
        },
    }
}

def main():
    validator = Draft7Validator(validators[sys.argv[2]])
    with open(sys.argv[1]) as f:
        data = json.loads(f.read())
        for error in validator.iter_errors(data):
            print(error.instance)
            print(json.dumps(error.message, indent=4))

# Validate the generated JSON Schema against the actual data (replace 'data' with your actual data)
if __name__ == "__main__":
    main()
