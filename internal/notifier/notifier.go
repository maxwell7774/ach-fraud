// Package notifier implements ports.Notifier. The no-op adapter is the
// default; SMTP (and later webhooks) can be added without touching the
// pipeline.
package notifier

import (
	"context"

	"github.com/27actions/ach/internal/domain"
)

// Noop discards every notification. It keeps the alerting boundary real while
// channels are unconfigured.
type Noop struct{}

func (Noop) Notify(context.Context, domain.Event) error { return nil }
