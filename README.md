[![Docker Repository on Quay](https://quay.io/repository/lwolf/konsumerator/status "Docker Repository on Quay")](https://quay.io/repository/lwolf/konsumerator)
[![Build Status](https://travis-ci.org/lwolf/konsumerator.svg?branch=master)](https://travis-ci.org/lwolf/konsumerator)
[![Go Report Card](https://goreportcard.com/badge/github.com/lwolf/konsumerator)](https://goreportcard.com/report/github.com/lwolf/konsumerator)
[![codecov](https://codecov.io/gh/lwolf/konsumerator/branch/master/graph/badge.svg)](https://codecov.io/gh/lwolf/konsumerator)

# Konsumerator

**Konsumerator** is a Kubernetes operator intended to automate management and resource allocations for 
kafka consumers. 

Operator creates and manages `Consumer` CRD, for this it requires cluster-wide permissions.

```yaml
apiVersion: konsumerator.lwolf.org/v1alpha1
kind: Consumer
metadata:
  name: consumer-sample
spec:
  numPartitions: 100
  numPartitionsPerInstance: 1
  name: "test-consumer"
  namespace: "default"
  autoscaler:
    # only one provider is supported yet - prometheus.
    # `prometheus` is configured using prometheus provider and user specific metrics
    mode: "prometheus"
    prometheus:
      # minimum allowed period to query prometheus for the lag
      # information to avoid DDoS of that service
      minSyncPeriod: "1m"
      # do not scale up if the lag is less than 5 minutes
      tolerableLag: "5m"
      # approximate consumption rate per CPU
      ratePerCore: 20000
      # approximate memory requirements per CPU
      # if ratePerCore is 10k ops, this value is amount of
      # RAM needed during the processing of this 10k ops
      ramPerCore: "100M"
      # criticalLag is some value close to the SLO.
      # if lag has reached this point, autoscaler will
      # give maximum allowed resource to that deployment
      criticalLag: "60m"
      # preferable recovery time. During lag, Consumer will try to allocate 
      # as much resources as possible to recover from lag during this period
      recoveryTime: "30m"
      # prometheus addresses to query
      address:
        - "http://prometheus-operator-prometheus.monitoring.svc.cluster.local:9090"
      # Offset query should return number of messages that is not
      # processed yet a.k.a lag per partitionLabel
      offset:
        query: "sum(rate(kafka_messages_last_offset{topic=''}[5m])) by (partition)"
        partitionLabel: "partition"
      # Production query should return number of messages is being
      # produced per partitionLabel per unit of time (second)
      production:
        query: "sum(rate(kafka_messages_produced_total{topic=''}[5m])) by (partition)"
        partitionLabel: "partition"
      # Consumption query should return number of messages is being
      # consumed per partitionLabel per unit of time (second)
      consumption:
        query: "sum(rate(kafka_messages_consumed_total{topic=''}[5m])) by (partition)"
        partitionLabel: "partition"
  # partitionEnvKey - the name of the environment variable
  # containing partition number the deployment is responsible for
  partitionEnvKey: "PARTITION"
  # DeploymentSpec to run the consumer
  deploymentTemplate:
    replicas: 1
    strategy:
      type: Recreate
    selector:
      matchLabels:
        app: my-dep
    template:
      metadata:
        labels:
          app: my-dep
      spec:
        containers:
          - image: busybox
            name: busybox-sidecar
            command: ["/bin/sh", "-ec", "env && sleep 3000"]
          - image: busybox
            name: busybox
            command: ["/bin/sh", "-ec", "sleep 2000"]
  # resource boundaries, this policy protects
  # the consumer from scaling to 0 or infinity in case
  # of incidents
  resourcePolicy:
    containerPolicies:
    - containerName: busybox
      minAllowed:
        cpu: "100m"
        memory: "100M"
      maxAllowed:
        cpu: "1"
        memory: "1G"
    - containerName: busybox-sidecar
      minAllowed:
        cpu: "100m"
        memory: "100M"
      maxAllowed:
        cpu: "100m"
        memory: "100M"
```

When such Consumer are being created, operator will create `.spec.numPartitions` unique deployments.

```bash
$ kubectl get consumers

  NAME              EXPECTED   RUNNING   PAUSED   MISSING   LAGGING   OUTDATED   AUTOSCALER   AGE
  consumer-sample   100        100       0        0         6         0          prometheus   6d2h


```

Deployment specifics:

* Deployment will be created based on the template provided in `.spec.deploymentTemplate` without any modifications.
It does not make sense to set resource field, since it will be overridden by autoscaler.
* Deployment will be named `{consumerName}-{index}` where consumerName is `.spec.name` and index is in range
from 0 to `.spec.numPartitions`. 
* Resource requests/limits for each deployment will be estimated based on metrics and configuration
* Each container in the deployment will get few environment variables set:
    `KONSUMERATOR_PARTITION` - contains comma-separated list of kafka partition numbers assigned to this deployment. Name of this variable is configurable. 
    `KONSUMERATOR_NUM_PARTITIONS` - total number of kafka partitions 
    `KONSUMERATOR_INSTANCE` - ordinal of the instance 
    `KONSUMERATOR_NUM_INSTANCES` - total number of instances 
    `GOMAXPROCS` - golang specific setting, always equals to the `resources.limit.cpu`. 

## Metrics Providers

At the moment only one metrics provider is implemented - Prometheus.

Here is a critical settings for the provider:

```yaml
spec:
  ...
  autoscaler:
    prometheus:
      # minimum allowed period to query prometheus for the lag
      # information to avoid DDoS of that service
      minSyncPeriod: "1m"
      address:
        - "http://prometheus-operator-prometheus.monitoring.svc.cluster.local:9090"
      # Offset query should return number of messages that is not
      # processed yet a.k.a lag per partitionLabel
      offset:
        query: "sum(rate(kafka_messages_last_offset{topic=''}[5m])) by (partition)"
        partitionLabel: "partition"
      # Production query should return number of messages is being
      # produced per partitionLabel per unit of time (second)
      production:
        query: "sum(rate(kafka_messages_produced_total{topic=''}[5m])) by (partition)"
        partitionLabel: "partition"
      # Consumption query should return number of messages is being
      # consumed per partitionLabel per unit of time (second)
      consumption:
        query: "sum(rate(kafka_messages_consumed_total{topic=''}[5m])) by (partition)"
        partitionLabel: "partition"
```


## Resource Predictors

At the moment only one resource predictor is implemented which is tightly coupled with Prometheus metrics
provider.
`NaivePredictor` operates using following settings provided by a user in `.spec.autoscaler.prometheus`
* `ratePerCore`- approximate number of message that could be processed by a single core during normal operations
* `ramPerCore` - approximate amount of RAM required for the amount of messages process by a core. Sometimes you don't have
such dependency between cpu and memory and want to allocate fixed amount of memory, in this case, put here any value and set resourcePolicy
for the container with minAllowed.memory = maxAllowed.memory.
* `recoveryTime` - if there is a lag of the partition, predictor will try to give consumer
as much resources as possible to recover during this period.
It also require `production` and `offset` metrics from the Prometheus. 

## Guest Mode (no cluster wide permissions)

Sometimes you don't have permissions to create CRDs in the cluster, for such cases Konsumerator supports
a so-called `guest-mode`. When running in guest-mode operator uses configmaps instead of CRDs and it is limited to a 
single namespace.
Guest-mode could be activated by setting namespace argument `konsumerator --namespace=default`.

To create an instance of the consumer, you need to create a ConfigMap with `konsumerator.lwolf.org/managed` annotation
and consumerSpec inside the body. 

```
apiVersion: v1
kind: ConfigMap
metadata:
  name: consumer-sample
  namespace: default
  annotations:
    konsumerator.lwolf.org/managed: "true"
data:
  consumer.yaml: |
    numPartitions: 100
    name: "test-consumer"
    namespace: "default"
    autoscaler:
      ...
```

## Installation

coming soon...

## Operations

### How to stop/start consumer

At the moment, the only way to stop the consumer is to set `.spec.deploymentTemplate.replicas` to 0.
This will trigger reconciliation, operator will notice that the deployment spec was changed and
apply the change, in this case change the number of replicas to 0. 


### How to pause auto scaling  

It is possible to temporary disable autoscaling of the managed deployments. 
To do so, add the following annotation to the `consumer` object with any value:

```yaml
annotations:
  konsumerator.lwolf.org/disable-autoscaler: "true"
```

In case of disabled autoscaling, operator will allocate minimum resources from the `resourcePolicy` for this container.
If there is no such policy set, no resource request/limit will be set.


## Development

Requirements

* https://github.com/kubernetes-sigs/kind - is used to spin-up dev cluster (v0.10+)
* https://github.com/etcd-io/etcd - etcd needs to be present in the system path. Kubebuilder is a using it to run integration tests locally
* https://github.com/kubernetes-sigs/kustomize - needs to be preset in the system path to render generated manifests 

### Dev k8s cluster

To spin up dev k8s cluster run the following:

```
make kind-create
```

This will create 2 node cluster (1 master and 1 node) with pre-installed Prometheus and Grafana.

#### Access cluster resources (Linux):
KIND is configured to expose 2 ports.
To get IP address of the KIND worker node, run:

```
kubectl get nodes konsumerator-worker -o jsonpath='{ $.status.addresses[?(@.type=="InternalIP")].address }'
```
then, you can access grafana and prometheus on the following ports of that IP address: 

* 30666 - prometheus
* 30777 - grafana (admin/admin)


`make build`

`make test`

`make install`

`make run`

