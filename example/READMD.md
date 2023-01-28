# Example
We can use this example to test.

First, create a deployment

```shell
kubectl apply -f nginx-deployment.yaml
```

Next, run server

```shell
go run main.go -v 4 --kubeconfig ~/.kube/config -deployment-name nginx-deployment -server
```

# Actions

**DO NOT CLOSE SERVER**

**Actions:**
* 1. **scale up**:
    ```shell
    go run main.go --kubeconfig ~/.kube/config -deployment-name nginx-deployment -mode scaleup
    ```
  * create a scale up for the deployment, plan steps: `1 --> 2 --> 5 --> 8 --> 10 --> 12 --> 15 --> 20`, and then the deployment replica stop at **8**.
* 2. **release**:
    ```shell
    go run main.go --kubeconfig ~/.kube/config -deployment-name nginx-deployment -mode release
    ```
  * continue scale up from stop point, plan steps: `8 --> 10 --> 12 --> 15 --> 20`
* 3. **stop**: 
  ```shell
  go run main.go --kubeconfig ~/.kube/config  -deployment-name nginx-deployment -mode stop
  ```
  * when the deployment begin release, stop the server, and then the deployment replica stop at **12**.
* 4. **continue release**:
    ```shell
    go run main.go --kubeconfig ~/.kube/config -deployment-name nginx-deployment -mode release
    ```
  * continue scale up from stop point, will do steps: `12 --> 15 --> 20`
* 5. **scale down**:
    ```shell
    go run main.go --kubeconfig ~/.kube/config  -deployment-name nginx-deployment -mode scaledown
    ```
  * scale down from 20 to 0,but includes a small scale up `12 --> 15`, all plan steps: `20 --> 12 --> 15 --> 10 --> 5 --> 1 --> 0`

**Output:**

```shell
for (( ; ; )) sleep 2 && kubectl get deployments.apps nginx-deployment -o jsonpath='{.metadata.annotations.current_step_index},{.metadata.annotations.current_step_state},{.metadata.annotations.steps} spec.replicas:{.spec.replicas},status.replicas:{.status.replicas},status.availableReplicas:{.status.availableReplicas}{"\n"}';
```
uniq log([full log](./log.txt)): 
```shell
,, spec.replicas:1,status.replicas:1,status.availableReplicas:1
2,StepUpgrade,[{"replicas":1},{"replicas":2},{"replicas":5},{"replicas":8,"pause":true},{"replicas":10},{"replicas":12},{"replicas":15},{"replicas":20}] spec.replicas:2,status.replicas:2,status.availableReplicas:1
3,StepUpgrade,[{"replicas":1},{"replicas":2},{"replicas":5},{"replicas":8,"pause":true},{"replicas":10},{"replicas":12},{"replicas":15},{"replicas":20}] spec.replicas:5,status.replicas:5,status.availableReplicas:2
3,StepUpgrade,[{"replicas":1},{"replicas":2},{"replicas":5},{"replicas":8,"pause":true},{"replicas":10},{"replicas":12},{"replicas":15},{"replicas":20}] spec.replicas:5,status.replicas:5,status.availableReplicas:4
4,StepPaused,[{"replicas":1},{"replicas":2},{"replicas":5},{"replicas":8,"pause":true},{"replicas":10},{"replicas":12},{"replicas":15},{"replicas":20}] spec.replicas:8,status.replicas:8,status.availableReplicas:5
5,StepUpgrade,[{"replicas":1},{"replicas":2},{"replicas":5},{"replicas":8,"pause":true},{"replicas":10},{"replicas":12},{"replicas":15},{"replicas":20}] spec.replicas:10,status.replicas:10,status.availableReplicas:5
5,StepUpgrade,[{"replicas":1},{"replicas":2},{"replicas":5},{"replicas":8,"pause":true},{"replicas":10},{"replicas":12},{"replicas":15},{"replicas":20}] spec.replicas:10,status.replicas:10,status.availableReplicas:6
6,StepUpgrade,[{"replicas":1},{"replicas":2},{"replicas":5},{"replicas":8,"pause":true},{"replicas":10},{"replicas":12},{"replicas":15},{"replicas":20}] spec.replicas:12,status.replicas:10,status.availableReplicas:10
6,StepUpgrade,[{"replicas":1},{"replicas":2},{"replicas":5},{"replicas":8,"pause":true},{"replicas":10},{"replicas":12},{"replicas":15},{"replicas":20}] spec.replicas:12,status.replicas:12,status.availableReplicas:10
6,StepPaused,[{"replicas":1},{"replicas":2},{"replicas":5},{"replicas":8,"pause":true},{"replicas":10},{"replicas":12,"pause":true},{"replicas":15},{"replicas":20}] spec.replicas:12,status.replicas:12,status.availableReplicas:10
6,StepPaused,[{"replicas":1},{"replicas":2},{"replicas":5},{"replicas":8,"pause":true},{"replicas":10},{"replicas":12,"pause":true},{"replicas":15},{"replicas":20}] spec.replicas:12,status.replicas:12,status.availableReplicas:11
7,StepUpgrade,[{"replicas":1},{"replicas":2},{"replicas":5},{"replicas":8,"pause":true},{"replicas":10},{"replicas":12,"pause":true},{"replicas":15},{"replicas":20}] spec.replicas:15,status.replicas:15,status.availableReplicas:12
8,StepUpgrade,[{"replicas":1},{"replicas":2},{"replicas":5},{"replicas":8,"pause":true},{"replicas":10},{"replicas":12,"pause":true},{"replicas":15},{"replicas":20}] spec.replicas:20,status.replicas:20,status.availableReplicas:15
8,StepUpgrade,[{"replicas":1},{"replicas":2},{"replicas":5},{"replicas":8,"pause":true},{"replicas":10},{"replicas":12,"pause":true},{"replicas":15},{"replicas":20}] spec.replicas:20,status.replicas:20,status.availableReplicas:17
8,Completed,[{"replicas":1},{"replicas":2},{"replicas":5},{"replicas":8,"pause":true},{"replicas":10},{"replicas":12,"pause":true},{"replicas":15},{"replicas":20}] spec.replicas:20,status.replicas:20,status.availableReplicas:20
1,StepUpgrade,[{"replicas":12},{"replicas":15},{"replicas":10},{"replicas":5},{"replicas":1},{}] spec.replicas:12,status.replicas:20,status.availableReplicas:20
2,StepUpgrade,[{"replicas":12},{"replicas":15},{"replicas":10},{"replicas":5},{"replicas":1},{}] spec.replicas:15,status.replicas:15,status.availableReplicas:12
2,StepUpgrade,[{"replicas":12},{"replicas":15},{"replicas":10},{"replicas":5},{"replicas":1},{}] spec.replicas:15,status.replicas:15,status.availableReplicas:13
6,Completed,[{"replicas":12},{"replicas":15},{"replicas":10},{"replicas":5},{"replicas":1},{}] spec.replicas:0,status.replicas:,status.availableReplicas:
```

```shell
kubectl get events --field-selector involvedObject.name=nginx-deployment -w --sort-by='.lastTimestamp'
```

```shell
7m45s       Normal   ScalingReplicaSet   deployment/nginx-deployment   Scaled up replica set nginx-deployment-6b474476c4 to 1
7m3s        Normal   ScalingReplicaSet   deployment/nginx-deployment   Scaled up replica set nginx-deployment-6b474476c4 to 2
6m25s       Normal   ScalingReplicaSet   deployment/nginx-deployment   Scaled up replica set nginx-deployment-6b474476c4 to 5
5m23s       Normal   ScalingReplicaSet   deployment/nginx-deployment   Scaled up replica set nginx-deployment-6b474476c4 to 8
5m16s       Normal   ScalingReplicaSet   deployment/nginx-deployment   Scaled up replica set nginx-deployment-6b474476c4 to 10
4m20s       Normal   ScalingReplicaSet   deployment/nginx-deployment   Scaled up replica set nginx-deployment-6b474476c4 to 12
2m47s       Normal   ScalingReplicaSet   deployment/nginx-deployment   Scaled up replica set nginx-deployment-6b474476c4 to 20
95s         Normal   ScalingReplicaSet   deployment/nginx-deployment   Scaled up replica set nginx-deployment-6b474476c4 to 15
95s         Normal   ScalingReplicaSet   deployment/nginx-deployment   Scaled down replica set nginx-deployment-6b474476c4 to 12
32s         Normal   ScalingReplicaSet   deployment/nginx-deployment   (combined from similar events): Scaled down replica set nginx-deployment-6b474476c4 to 0
```

