ALTER TABLE applications DROP CONSTRAINT IF EXISTS applications_allowed_origins_shape;
DROP FUNCTION IF EXISTS origins_are_well_formed(text[]);
ALTER TABLE applications DROP COLUMN IF EXISTS allowed_origins;
