package app

import "github.com/dougkusanagi/dev-lan/internal/application"

// These aliases preserve the app package API while moving the transport-
// neutral command result and settings DTOs to the application layer.
type ApplyResult = application.ApplyResult
type GlobalSettings = application.GlobalSettings
