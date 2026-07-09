package redis

const keyPrefix = "gfire:"

func jobKey(id string) string       { return keyPrefix + "job:" + id }
func jobStatesKey(id string) string { return keyPrefix + "job:" + id + ":states" }
func queueKey(name string) string   { return keyPrefix + "queue:" + name }
func stateIndexKey(state string) string {
	return keyPrefix + "state:" + state
}

const (
	processingKey    = keyPrefix + "processing"
	scheduledKey     = keyPrefix + "scheduled"
	recurringKey     = keyPrefix + "recurring"
	serversKey       = keyPrefix + "servers"
	serverHBKey      = keyPrefix + "server:heartbeats"
	queuesIndexKey   = keyPrefix + "queues"
	counterKeyPrefix = keyPrefix + "counter:"
	lockKeyPrefix    = keyPrefix + "lock:"
)

func counterKey(name string) string { return counterKeyPrefix + name }
func lockKey(resource string) string {
	return lockKeyPrefix + resource
}
func continuationsKey(parentID string) string {
	return keyPrefix + "continuations:" + parentID
}
func continuationEntryKey(parentID, entryID string) string {
	return keyPrefix + "continuation:" + parentID + ":" + entryID
}
