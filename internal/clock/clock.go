// Package clock implements ports.Clock.
package clock

import "time"

// Real is the production clock.
type Real struct{}

func (Real) Now() time.Time { return time.Now() }
