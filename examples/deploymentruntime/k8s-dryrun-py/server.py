"""The DeploymentRuntimeService gRPC implementation (Python) — k8s-dryrun runtime.

Same surface as the local-process reference; only the backend differs (it admits a Pod
manifest instead of forking a process). RequestContext is NOT a field (ADR-007).
"""

from rat.deploymentruntime.v1 import deployment_runtime_pb2, deployment_runtime_pb2_grpc

from store import DeploymentError, K8sDryRunRuntime

_STATUS = {
    "HEALTHY": deployment_runtime_pb2.HealthStatus.HEALTH_STATUS_HEALTHY,
    "UNHEALTHY": deployment_runtime_pb2.HealthStatus.HEALTH_STATUS_UNHEALTHY,
    "UNKNOWN": deployment_runtime_pb2.HealthStatus.HEALTH_STATUS_UNKNOWN,
}


class DeploymentRuntimeServicer(deployment_runtime_pb2_grpc.DeploymentRuntimeServiceServicer):
    def __init__(self, runtime=None) -> None:
        self.rt = runtime or K8sDryRunRuntime()

    def Launch(self, request, context):
        try:
            iid, endpoint = self.rt.launch(request.plugin_id, request.spec)
        except DeploymentError as e:
            context.abort(e.code, e.message)
        return deployment_runtime_pb2.LaunchResponse(instance_id=iid, endpoint=endpoint)

    def Terminate(self, request, context):
        return deployment_runtime_pb2.TerminateResponse(
            terminated=self.rt.terminate(request.instance_id))

    def Healthcheck(self, request, context):
        status, detail = self.rt.healthcheck(request.instance_id)
        return deployment_runtime_pb2.HealthcheckResponse(status=_STATUS[status], detail=detail)
