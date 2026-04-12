-- Spanner benchmark seed data (Tier A: small).
--
-- For Tier B (10K singers, 50K albums) and Tier C (100K singers, 500K albums),
-- use scripts/seed_spanner.sh which generates deterministic data at scale.

INSERT INTO Singers (SingerId, FirstName, LastName, BirthDate) VALUES
  (1, 'Marc', 'Richards', '1970-09-03'),
  (2, 'Catalina', 'Smith', '1990-08-17'),
  (3, 'Alice', 'Trentor', '1991-10-02'),
  (4, 'Lea', 'Martin', '1991-11-09'),
  (5, 'David', 'Lomond', '1977-01-29');

INSERT INTO Albums (SingerId, AlbumId, AlbumTitle, ReleaseYear) VALUES
  (1, 1, 'Total Junk', 2011),
  (1, 2, 'Go, Go, Go', 2012),
  (2, 1, 'Green', 2013),
  (2, 2, 'Forever Hold Your Peace', 2014),
  (3, 1, 'Terrified', 2015),
  (4, 1, 'Not About The Guitar', 2016),
  (5, 1, 'The Second Time', 2017),
  (5, 2, 'Mayber Maybe Not', 2018);
