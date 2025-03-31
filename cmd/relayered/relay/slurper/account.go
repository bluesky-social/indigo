package slurper

var (
	// AccountStatusActive is not in the spec but used internally
	// the alternative would be an additional SQL column for "active" or status="" to imply active
	AccountStatusActive = "active"

	AccountStatusDeactivated    = "deactivated"
	AccountStatusDeleted        = "deleted"
	AccountStatusDesynchronized = "desynchronized"
	AccountStatusSuspended      = "suspended"
	AccountStatusTakendown      = "takendown"
	AccountStatusThrottled      = "throttled"
)

var AccountStatusList = []string{
	AccountStatusActive,
	AccountStatusDeactivated,
	AccountStatusDeleted,
	AccountStatusDesynchronized,
	AccountStatusSuspended,
	AccountStatusTakendown,
	AccountStatusThrottled,
}
var AccountStatuses map[string]bool

func init() {
	AccountStatuses = make(map[string]bool, len(AccountStatusList))
	for _, status := range AccountStatusList {
		AccountStatuses[status] = true
	}
}
