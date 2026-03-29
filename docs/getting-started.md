# Getting Started

## Install

### Option 1: Download binary

```bash
# Linux amd64
curl -L https://github.com/srjn45/filedbv2/releases/latest/download/filedbv2_linux_amd64.tar.gz | tar xz
sudo mv filedb filedb-cli /usr/local/bin/
```

### Option 2: Docker

```bash
docker run -d \
  -p 5433:5433 -p 8080:8080 \
  -v $(pwd)/data:/data \
  -e FILEDB_API_KEY=my-secret-key \
  ghcr.io/srjn45/filedbv2:latest
```

### Option 3: Build from source

```bash
git clone https://github.com/srjn45/filedbv2
cd filedbv2
make build
# binaries: bin/filedb and bin/filedb-cli
```

---

## Start the server

```bash
filedb serve \
  --data ./data \
  --api-key my-secret-key \
  --grpc-addr :5433 \
  --rest-addr :8080
```

Environment variable alternative:

```bash
export FILEDB_API_KEY=my-secret-key
filedb serve --data ./data
```

---

## Use the CLI

### Interactive REPL

```bash
export FILEDB_API_KEY=my-secret-key
filedb-cli repl

filedb> create-collection users
filedb> use users
filedb:users> insert {"name":"alice","age":30}
→ inserted id:1

filedb:users> find
→ id:1  {"name":"alice","age":30}

filedb:users> find {"field":"name","op":"eq","value":"alice"}
→ id:1  {"name":"alice","age":30}

filedb:users> update 1 {"name":"alice","age":31}
→ updated id:1

filedb:users> stats
→ collection:users  records:1  segments:1  dirty:0  size:89 bytes

filedb:users> delete 1
→ deleted id:1
```

### One-shot commands

```bash
filedb-cli create-collection products
filedb-cli insert products '{"name":"widget","price":9.99}'
filedb-cli find products '{"field":"price","op":"lte","value":"10.00"}'
filedb-cli get products 1
filedb-cli update products 1 '{"name":"widget","price":8.99}'
filedb-cli delete products 1
filedb-cli stats products
```

### Batch script (.fql)

```bash
# seed.fql
# Create users collection and seed data
create-collection users
use users
insert {"name":"alice","email":"alice@example.com"}
insert {"name":"bob","email":"bob@example.com"}
insert {"name":"carol","email":"carol@example.com"}
```

```bash
filedb-cli run seed.fql
# or via pipe:
cat seed.fql | filedb-cli run
```

### Export / Import

```bash
# Export to NDJSON
filedb-cli export users > users_backup.ndjson

# Import from NDJSON
cat users_backup.ndjson | filedb-cli import users
```

---

## Use via REST API

```bash
# Create collection
curl -X POST http://localhost:8080/v1/collections \
  -H "x-api-key: my-secret-key" \
  -H "Content-Type: application/json" \
  -d '{"name":"users"}'

# Insert
curl -X POST http://localhost:8080/v1/users/records \
  -H "x-api-key: my-secret-key" \
  -H "Content-Type: application/json" \
  -d '{"data":{"name":"alice","age":30}}'

# Get by id
curl http://localhost:8080/v1/users/records/1 \
  -H "x-api-key: my-secret-key"

# Update
curl -X PUT http://localhost:8080/v1/users/records/1 \
  -H "x-api-key: my-secret-key" \
  -H "Content-Type: application/json" \
  -d '{"data":{"name":"alice","age":31}}'

# Delete
curl -X DELETE http://localhost:8080/v1/users/records/1 \
  -H "x-api-key: my-secret-key"
```

---

## Filter syntax

Filters are JSON objects passed to `find` commands or the `POST /v1/{collection}/records/find` endpoint.

### Field filter

```json
{"field": "name", "op": "eq",       "value": "alice"}
{"field": "age",  "op": "gte",      "value": "18"}
{"field": "bio",  "op": "contains", "value": "engineer"}
{"field": "email","op": "regex",    "value": ".*@gmail\\.com"}
```

Supported operators: `eq`, `neq`, `gt`, `gte`, `lt`, `lte`, `contains`, `regex`

### Composite filters

```json
{
  "and": [
    {"field": "age",  "op": "gte", "value": "18"},
    {"field": "city", "op": "eq",  "value": "New York"}
  ]
}
```

```json
{
  "or": [
    {"field": "role", "op": "eq", "value": "admin"},
    {"field": "role", "op": "eq", "value": "superuser"}
  ]
}
```
