package main

import (
	auditlogv1 "github.com/rat-dev/rat/gen/rat/auditlog/v1"
	billingv1 "github.com/rat-dev/rat/gen/rat/billing/v1"
	catalogv1 "github.com/rat-dev/rat/gen/rat/catalog/v1"
	deploymentruntimev1 "github.com/rat-dev/rat/gen/rat/deploymentruntime/v1"
	enginev1 "github.com/rat-dev/rat/gen/rat/engine/v1"
	formatv1 "github.com/rat-dev/rat/gen/rat/format/v1"
	identityv1 "github.com/rat-dev/rat/gen/rat/identity/v1"
	marketplacev1 "github.com/rat-dev/rat/gen/rat/marketplace/v1"
	notificationsv1 "github.com/rat-dev/rat/gen/rat/notifications/v1"
	observabilityv1 "github.com/rat-dev/rat/gen/rat/observability/v1"
	runtimev1 "github.com/rat-dev/rat/gen/rat/runtime/v1"
	schedulerv1 "github.com/rat-dev/rat/gen/rat/scheduler/v1"
	secretv1 "github.com/rat-dev/rat/gen/rat/secret/v1"
	statev1 "github.com/rat-dev/rat/gen/rat/state/v1"
	storagev1 "github.com/rat-dev/rat/gen/rat/storage/v1"
	strategyv1 "github.com/rat-dev/rat/gen/rat/strategy/v1"
	tenancyv1 "github.com/rat-dev/rat/gen/rat/tenancy/v1"
	uiv1 "github.com/rat-dev/rat/gen/rat/ui/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// routableDescriptors is the union of axis service descriptors the gateway needs to
// derive its capability→method route table (gateway.buildRoutes reads the
// (rat.common.v1.capability) annotation off each method). The daemon passes the
// union of every axis a plane may route — extra descriptors are harmless (they just
// add unused routes). Importing every axis here also populates protoregistry.GlobalFiles,
// which is what `rat plugin init`'s kind-aware scaffold derives provides + servicer stubs
// from (Gaps 4/8a) — so the scaffold knows every axis, not just the data plane.
func routableDescriptors() []protoreflect.FileDescriptor {
	return []protoreflect.FileDescriptor{
		// data plane
		statev1.File_rat_state_v1_state_proto,
		catalogv1.File_rat_catalog_v1_catalog_proto,
		enginev1.File_rat_engine_v1_engine_proto,
		formatv1.File_rat_format_v1_format_proto,
		storagev1.File_rat_storage_v1_storage_proto,
		strategyv1.File_rat_strategy_v1_strategy_proto,
		runtimev1.File_rat_runtime_v1_runtime_proto,
		deploymentruntimev1.File_rat_deploymentruntime_v1_deployment_runtime_proto,
		// control plane
		secretv1.File_rat_secret_v1_secret_proto,
		identityv1.File_rat_identity_v1_identity_proto,
		schedulerv1.File_rat_scheduler_v1_scheduler_proto,
		tenancyv1.File_rat_tenancy_v1_tenancy_proto,
		billingv1.File_rat_billing_v1_billing_proto,
		observabilityv1.File_rat_observability_v1_observability_proto,
		auditlogv1.File_rat_auditlog_v1_auditlog_proto,
		// experience
		uiv1.File_rat_ui_v1_ui_proto,
		notificationsv1.File_rat_notifications_v1_notifications_proto,
		marketplacev1.File_rat_marketplace_v1_marketplace_proto,
	}
}
