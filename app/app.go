// Package app is the composition root for the device ingestion service.
package app

import "context"

// Run builds the service runtime and owns it until shutdown.
func Run(ctx context.Context, config Config) error {
	runtime, err := newRuntime(config)
	if err != nil {
		return err
	}
	return runtime.Run(ctx)
}
