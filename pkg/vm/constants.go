package vm

// VM status constants
const (
	VMStatusCreated      = "created"
	VMStatusInitializing = "initializing" // First boot, cloud-init running
	VMStatusStarting     = "starting"     // Starting up, already initialized
	VMStatusStarted      = "started"      // Running and ready for use
	VMStatusStopped      = "stopped"      // Stopped (was not initialized)
	VMStatusReady        = "ready"        // Stopped (was initialized)
	VMStatusFailed       = "failed"       // Failed to start or initialize
	VMStatusDeleted      = "deleted"      // Deleted
)
