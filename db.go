package main

import "time"

type redisDb struct {
	//dict    map[string]*robj
	//expires map[string]int64
	dict    dict
	expires dict
	id      int
}

func lookupKeyWriteOrReply(c *redisClient, key *robj, reply *string) *robj {
	o := lookupKeyWrite(c.db, key)
	if o == nil {
		addReply(c, reply)
	}

	return o
}

func lookupKeyWrite(db *redisDb, key *robj) *robj {
	//check if the key has expired, and if so, delete it.
	expireIfNeeded(db, key)
	//query the dictionary for the value corresponding to the key.
	return lookupKey(db, key)
}

func expireIfNeeded(db *redisDb, key *robj) int {
	//get the expiration time of the key.
	//when, exists := db.expires[(*key.ptr).(string)]
	entry := dictFind(&db.expires, (*key.ptr).(string))

	if entry == nil {
		return 0
	}

	when := (*entry.val.ptr).(int64)
	if when < 0 {
		return 0
	}
	now := time.Now().UnixMilli()
	//if the current time is less than the expiration time, it means the current key has not expired, so return directly.
	if now < when {
		return 0
	}
	//delete expired keys.
	dbDelete(db, key)

	return 1

}

func dbDelete(db *redisDb, key *robj) {
	//delete(db.expires, (*key.ptr).(string))
	//delete(db.dict, (*key.ptr).(string))
	dictDelete(&db.dict, (*key.ptr).(string))
	dictDelete(&db.expires, (*key.ptr).(string))
}

func lookupKeyRead(db *redisDb, key *robj) *robj {
	//check if the key has expired and delete it.
	expireIfNeeded(db, key)
	val := lookupKey(db, key)
	return val
}

func lookupKey(db *redisDb, key *robj) *robj {
	//val := db.dict[(*key.ptr).(string)]
	de := dictFind(&db.dict, (*key.ptr).(string))
	if de == nil {
		return nil
	}
	return de.val
}

func lookupKeyReadOrReply(c *redisClient, key *robj, reply *string) *robj {
	//check if the key has expired to decide whether to delete the key from the dictionary, then query the dictionary for the result and return it.
	o := lookupKeyRead(c.db, key)
	if o == nil {
		addReply(c, reply)
	}
	return o
}

func dbAdd(db *redisDb, key *robj, val *robj) {
	//db.dict[(*key.ptr).(string)] = val
	dictAdd(&db.dict, key, val)
}

func dbOverwrite(db *redisDb, key *robj, val *robj) {
	de := dictFind(&db.dict, (*key.ptr).(string))
	if de == nil {
		panic("de is null")
	}
	dictReplace(&db.dict, key, val)
}

func scanCallback(privData [2]any, de *dictEntry) {
	keys := privData[0].(*list)
	o := privData[1].(*robj)

	var key, value interface{}

	if o == nil {
		sdsKey := dictGetKey(de)
		key = interface{}(sdsKey)
	}

	listAddNodeTail(keys, &key)

	if value != nil {
		listAddNodeTail(keys, &value)
	}

}
