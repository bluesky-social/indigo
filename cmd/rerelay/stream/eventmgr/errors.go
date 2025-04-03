package eventmgr

import (
	"fmt"
)

var (
	ErrPlaybackShutdown = fmt.Errorf("playback shutting down")
	ErrCaughtUp         = fmt.Errorf("caught up")
)
