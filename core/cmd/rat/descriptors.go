package main

import (
	catalogv1 "github.com/rat-dev/rat/gen/rat/catalog/v1"
	enginev1 "github.com/rat-dev/rat/gen/rat/engine/v1"
	formatv1 "github.com/rat-dev/rat/gen/rat/format/v1"
	secretv1 "github.com/rat-dev/rat/gen/rat/secret/v1"
	statev1 "github.com/rat-dev/rat/gen/rat/state/v1"
	storagev1 "github.com/rat-dev/rat/gen/rat/storage/v1"
	strategyv1 "github.com/rat-dev/rat/gen/rat/strategy/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// routableDescriptors is the union of axis service descriptors the gateway needs to
// derive its capability→method route table (gateway.buildRoutes reads the
// (rat.common.v1.capability) annotation off each method). The daemon passes the
// union of every axis a plane may route — extra descriptors are harmless (they just
// add unused routes). This v1 set covers the data-plane axes the Phase-A test plane
// (state) and the Phase-B data-dev plane (catalog/engine/format/storage/strategy)
// exercise; new axes are one import + one line.
func routableDescriptors() []protoreflect.FileDescriptor {
	return []protoreflect.FileDescriptor{
		statev1.File_rat_state_v1_state_proto,
		catalogv1.File_rat_catalog_v1_catalog_proto,
		enginev1.File_rat_engine_v1_engine_proto,
		formatv1.File_rat_format_v1_format_proto,
		storagev1.File_rat_storage_v1_storage_proto,
		strategyv1.File_rat_strategy_v1_strategy_proto,
		secretv1.File_rat_secret_v1_secret_proto,
	}
}
