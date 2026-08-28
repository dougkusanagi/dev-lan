package app

import "github.com/dougkusanagi/dev-lan/internal/application"

// These aliases preserve the app package API while moving the transport-
// neutral command result and settings DTOs to the application layer.
type ApplyResult = application.ApplyResult
type GlobalSettings = application.GlobalSettings
type APIEndpointFiles = application.EndpointFiles
type OperationState = application.OperationState
type PHPVersionStatus = application.PHPVersionStatus
type RouteAllocation = application.RouteAllocation
type TopologySnapshot = application.TopologySnapshot
type FirewallSnapshot = application.FirewallSnapshot
type UninstallOptions = application.UninstallOptions
type UninstallAction = application.UninstallAction
type UninstallItem = application.UninstallItem
type UninstallPlan = application.UninstallPlan
type UninstallResult = application.UninstallResult

const (
	UninstallRemove   = application.UninstallRemove
	UninstallRestore  = application.UninstallRestore
	UninstallPreserve = application.UninstallPreserve
	UninstallConflict = application.UninstallConflict
	UninstallPending  = application.UninstallPending
	UninstallFailed   = application.UninstallFailed
)

// Check remains the legacy app-package shape for callers that still consume
// App.Doctor directly. Transport reads use application.DoctorCheck through the
// explicit DoctorSnapshot adapter.
type Check struct {
	Name   string
	Status string
	Detail string
}
