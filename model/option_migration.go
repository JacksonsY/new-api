package model

// reliabilityDefaultsMigrationKey is retained only for compatibility with
// deployments that already wrote the old marker.
const reliabilityDefaultsMigrationKey = "MigrationReliabilityDefaultsV1"

// migrateLegacyReliabilityDefaults deliberately preserves persisted values.
// The Option table records no provenance, so a value equal to a historical
// default may still be an administrator's explicit configuration.
func migrateLegacyReliabilityDefaults() {}
