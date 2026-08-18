package relay

import (
	"time"

	"github.com/RussellLuo/slidingwindow"
)

func perDayLimiter(count int64) *slidingwindow.Limiter {
	lim, _ := slidingwindow.NewLimiter(time.Hour*24, count, func() (slidingwindow.Window, slidingwindow.StopFunc) {
		return slidingwindow.NewLocalWindow()
	})
	return lim
}
