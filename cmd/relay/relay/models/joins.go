package models

type AccountDetailed struct {
	UID            uint64        `gorm:"column:uid"`
	DID            string        `gorm:"column:did"`
	HostID         uint64        `gorm:"column:host_id"`
	Status         AccountStatus `gorm:"column:status"`
	UpstreamStatus AccountStatus `gorm:"column:upstream_status"`
	Rev            string        `gorm:"column:rev"`
	CommitCID      string        `gorm:"column:commit_cid"`
	CommitDataCID  string        `gorm:"column:commit_data_cid"`
}

func (a *AccountDetailed) AccountStatus() AccountStatus {
	if a.Status != AccountStatusActive {
		if a.Status == AccountStatusHostThrottled {
			return AccountStatusThrottled
		}
		return a.Status
	}
	return a.UpstreamStatus
}

// Returns a pointer to a copy of status string; or nil if status is active.
//
// Helpful for account info responses which have a boolean 'active' and optional 'status' field (like the #account message)
func (a *AccountDetailed) StatusField() *string {
	if a.IsActive() {
		return nil
	}
	s := string(a.AccountStatus())
	return &s
}

func (a *AccountDetailed) IsActive() bool {
	return (a.Status == AccountStatusActive || a.Status == AccountStatusThrottled) && a.UpstreamStatus == AccountStatusActive
}
