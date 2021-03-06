package runnerpool

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/fnproject/fn/api/models"

	"github.com/fnproject/fn/api/common"
	"github.com/sirupsen/logrus"
)

type naivePlacer struct {
	rrInterval time.Duration
	rrIndex    uint64
}

func NewNaivePlacer() Placer {
	rrIndex := uint64(time.Now().Nanosecond())
	logrus.Infof("Creating new naive runnerpool placer rrIndex=%d", rrIndex)
	return &naivePlacer{
		rrInterval: 10 * time.Millisecond,
		rrIndex:    rrIndex,
	}
}

func (sp *naivePlacer) PlaceCall(rp RunnerPool, ctx context.Context, call RunnerCall) error {

	log := common.Logger(ctx)
	for {
		runners, err := rp.Runners(call)
		if err != nil {
			log.WithError(err).Error("Failed to find runners for call")
		} else {
			for j := 0; j < len(runners); j++ {

				select {
				case <-ctx.Done():
					return models.ErrCallTimeoutServerBusy
				default:
				}

				i := atomic.AddUint64(&sp.rrIndex, uint64(1))
				r := runners[int(i)%len(runners)]

				tryCtx, tryCancel := context.WithCancel(ctx)
				placed, err := r.TryExec(tryCtx, call)
				tryCancel()

				if err != nil && err != models.ErrCallTimeoutServerBusy {
					log.WithError(err).Error("Failed during call placement")
				}
				if placed {
					return err
				}
			}
		}

		// backoff
		select {
		case <-ctx.Done():
			return models.ErrCallTimeoutServerBusy
		case <-time.After(sp.rrInterval):
		}
	}
}
