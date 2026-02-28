package types

import "context"

// TagKeeper defines the interface for tag registry operations.
// x/forum implements this interface. Other modules (x/rep, x/collect, x/blog)
// use it to validate tags against the shared registry.
type TagKeeper interface {
	// TagExists checks if a tag exists in the registry.
	TagExists(ctx context.Context, name string) (bool, error)

	// IsReservedTag checks if a tag is reserved.
	IsReservedTag(ctx context.Context, name string) (bool, error)

	// GetTag returns the tag metadata. Returns error if not found.
	GetTag(ctx context.Context, name string) (Tag, error)

	// IncrementTagUsage increments the usage count and updates last_used_at.
	// Called by other modules when they use a tag (e.g., creating an initiative with tags).
	IncrementTagUsage(ctx context.Context, name string, timestamp int64) error
}
