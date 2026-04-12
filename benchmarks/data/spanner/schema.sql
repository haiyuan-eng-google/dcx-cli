-- Spanner benchmark schema.
--
-- Based on the official Singers/Albums getting-started schema.
-- Apply via: gcloud spanner databases ddl update

CREATE TABLE Singers (
  SingerId   INT64 NOT NULL,
  FirstName  STRING(1024),
  LastName   STRING(1024),
  BirthDate  DATE,
) PRIMARY KEY(SingerId);

CREATE TABLE Albums (
  SingerId   INT64 NOT NULL,
  AlbumId    INT64 NOT NULL,
  AlbumTitle STRING(MAX),
  ReleaseYear INT64,
) PRIMARY KEY(SingerId, AlbumId),
  INTERLEAVE IN PARENT Singers ON DELETE CASCADE;
