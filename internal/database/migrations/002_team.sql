CREATE TABLE team_sources (id TEXT NOT NULL, data TEXT NOT NULL);
CREATE INDEX team_sources_id ON team_sources(id);
CREATE TABLE local_metadata (id TEXT NOT NULL, data BLOB NOT NULL);
CREATE INDEX local_metadata_id ON local_metadata(id);
