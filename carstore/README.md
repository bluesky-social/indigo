# Carstore

Store a zillion users of PDS-like repo, with more limited operations (mainly: firehose in, firehose out)

## Sqlite3 store

Experimental/demo.

```sql
CREATE TABLE IF NOT EXISTS blocks (uid int, cid blob, rev varchar, root blob, block blob, PRIMARY KEY(uid,cid))
CREATE INDEX IF NOT EXISTS blocx_by_rev ON blocks (uid, rev DESC)

INSERT INTO blocks (uid, cid, rev, root, block) VALUES (?, ?, ?, ?, ?) ON CONFLICT (uid,cid) DO UPDATE SET rev=excluded.rev, root=excluded.root, block=excluded.block

SELECT rev, root FROM blocks WHERE uid = ? ORDER BY rev DESC LIMIT 1

SELECT cid,rev,root,block FROM blocks WHERE uid = ? AND rev > ? ORDER BY rev DESC

DELETE FROM blocks WHERE uid = ?

SELECT rev, root FROM blocks WHERE uid = ? AND cid = ? LIMIT 1

SELECT block FROM blocks WHERE uid = ? AND cid = ? LIMIT 1

SELECT length(block) FROM blocks WHERE uid = ? AND cid = ? LIMIT 1
```
