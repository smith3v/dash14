# Season Team Catalog Design

## Goal

Prepare the tracked team-import catalog for the 2026–2027 Heren 2e Klasse C
season. The catalog is the only data source changed in this phase; importing it
later remains the application's existing responsibility.

## Scope

Keep every current `teams/teams.yaml` record exactly as it is, then append the
thirteen unique teams from the official Spaarnestad HS 11 league and cup
standings. Each record uses the existing import schema: `key`, `name`,
`short_name`, `hometown`, `logo`, and `aliases`.

The importer is intentionally unchanged. A `logo` value is a source file for
the import operation, so records for two teams from the same club will point to
the same source asset in `teams/logos`. The present importer may create
per-team runtime copies in `runtime/data/logos`; that is accepted for this
rollout. Existing logo files, including former-team filenames, remain in place
and are reused where they represent the same club.

Only clubs without a source asset receive one new official Nevobo logo asset:
Albatros, Compaen, VCC'92, The Setfighters, and VVM'63. No database rows,
game history, users, subscriptions, or runtime logo files are deleted or
edited.

## Verification

Validate the YAML through the existing importer parser and run the importer
tests. Inspect the resulting list to confirm all thirteen new records have
complete metadata and that shared-club records reference the same tracked
source file.
The actual `--import` execution against the production copy is deliberately
outside this preparation change.
