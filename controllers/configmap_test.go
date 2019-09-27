package controllers

import (
	"bytes"
	"fmt"
	konsumeratorv1alpha1 "github.com/lwolf/konsumerator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"testing"
)

func TestConfigMap(t *testing.T) {
	data := []byte(`
numPartitions: 2
name: "test-consumer"
namespace: "default"
autoscaler:
  # mode could be vpa or prometheus.
  mode: "prometheus"
  prometheus:
    # minimum allowed period to query prometheus for the lag
    # information to avoid DDoS of that service
    minSyncPeriod: "1m"
    # do not scale up if the lag is less than 5 minutes
    tolerableLag: "5m"
    # approximate consumption rate per CPU
    ratePerCore: 50000
    # approximate memory requirements per CPU
    # if ratePerCore is 10k ops, this value is amount of
    # RAM needed during the processing of this 10k ops
    ramPerCore: "100M"
    # criticalLag is some value close to the SLO.
    # if lag has reached this point, autoscaler will
    # give maximum allowed resource to that deployment
    criticalLag: "60m"
    # preferable recovery time. During lag, if resources are
    # available, consumer will be scaled up to recover during
    # during this time.
    recoveryTime: "30m"
    # prometheus addresses to query
    address:
#        - "http://prometheus-server.kube-system:9090"
      - "http://172.17.0.3:30666"
    # Offset query should return number of messages that is not
    # processed yet a.k.a lag per partitionLabel
    offset:
      query: "max(konsumerator_messages_production_offset) by (partition) - max(konsumerator_messages_consumption_offset) by (partition)"
      partitionLabel: "partition"
    # Production query should return number of messages is being
    # produced per partitionLabel per unit of time (second)
    production:
      query: "sum(rate(konsumerator_messages_production_offset[2m])) by (partition)"
      partitionLabel: "partition"
    # Consumption query should return number of messages is being
    # consumed per partitionLabel per unit of time (second)
    consumption:
      query: "sum(rate(konsumerator_messages_consumption_offset[2m])) by (partition)"
      partitionLabel: "partition"
# partitionEnvKey - the name of the environment variable
# containing partition number the deployment is responsible for
# partitionEnvKey: "PARTITION"
# DeploymentSpec to run the consumer
deploymentTemplate:
  replicas: 1
  strategy:
    type: Recreate
  selector:
    matchLabels:
      app: faker
      tier: consumer
  template:
    metadata:
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "9000"
      labels:
        app: faker
        tier: consumer
    spec:
      containers:
        - image: busybox
          name: busybox-info
          command: ["/bin/sh", "-ec", "env && sleep 30000"]
        - image: lwolf/faker:latest
          name: consumer
          command:
            - /faker
            - consumer
            - --rpc=50000
            - --redisAddr=redis.default:6379
            - --port=9000
          ports:
            - containerPort: 9000
              name: metrics
              protocol: TCP
          env:
            - name: "TEST_KEY"
              value: "test-value"
# resource boundaries, this optional policy protects
# the consumer from scaling to 0 or infinity in case
# of incidents
resourcePolicy:
  globalPolicy:
    maxAllowed:
      cpu: "1200"
      memory: "1.2T"
  containerPolicies:
  - containerName: consumer
    minAllowed:
      cpu: "100m"
      memory: "100M"
    maxAllowed:
      cpu: "1"
      memory: "100M"
  - containerName: busybox-info
#      mode: Off
    minAllowed:
      cpu: "100m"
      memory: "100M"
    maxAllowed:
      cpu: "100m"
      memory: "100M"
`)

	var consumer konsumeratorv1alpha1.Consumer
	br := bytes.NewReader(data)
	d := yaml.NewYAMLToJSONDecoder(br)
	if err := d.Decode(&consumer.Spec); err != nil {
		t.Fatalf("unexp err: %s", err)
	}
	fmt.Println(consumer.Spec)
	fmt.Println(consumer.Spec.Autoscaler.Prometheus)
}
