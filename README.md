# Drift

A scheam-first postgres cli migration tool.

## Why?

The way I like to work when it comes to database migrations is to have a single
`schema.sql` file that represents the current state of my database. I would
then update that file and run a tool to diff it against the result execution of
all pre-existing migrations. This presents some advantages, mainly enabling you
to see the databse state without needing to open a connection or trying to
parse existing migrations. This issue with
[ariga/atlas](https://github.com/ariga/atlas) is it don't generate some schema
objects (or require pro account?!) like triggers and functions and is simply an
overkill sometimes.

## Usage

```bash
drift -name "add_users_table"

# -migrations : migration directory, default to "./migrations/"
# -schema : schema file, default to "./schema.sql"
```

## How it works

Drift uses [stripe/pg-schema-diff](https://github.com/stripe/pg-schema-diff)
under the hood. It applies all existing migrations to a temporary database
using `testcontainers`. This mean you need to have docker to use drift.
`pg-schema-diff` is then used to generate the diff migration by taking the schema
content as target and temporary database as source.

Of course your schema file cannot live in the migrations directory simply because it will
be treated as a migration.

## TODO

- [ ] add tests
