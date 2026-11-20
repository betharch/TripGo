package positions

import "time"

type Ticker struct {
	ticker *time.Ticker
}

func NewTicker(period time.Duration) *Ticker {
	return &Ticker{ticker: time.NewTicker(period)}
}

func (t *Ticker) C() <-chan time.Time {
	return t.ticker.C
}

func (t *Ticker) Stop() {
	t.ticker.Stop()
}
