package relay

type HostRates struct {
	// core event rate, counts firehose events
	PerSecond int64 `json:"per_second,omitempty"`
	PerHour   int64 `json:"per_hour,omitempty"`
	PerDay    int64 `json:"per_day,omitempty"`

	RepoLimit int64 `json:"repo_limit,omitempty"`
}

func (pr *HostRates) FromSlurper(s *Slurper) {
	if pr.PerSecond == 0 {
		pr.PerHour = s.Config.DefaultPerSecondLimit
	}
	if pr.PerHour == 0 {
		pr.PerHour = s.Config.DefaultPerHourLimit
	}
	if pr.PerDay == 0 {
		pr.PerDay = s.Config.DefaultPerDayLimit
	}
	if pr.RepoLimit == 0 {
		pr.RepoLimit = s.Config.DefaultRepoLimit
	}
}
