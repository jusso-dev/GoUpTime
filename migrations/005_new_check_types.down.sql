ALTER TABLE check_results DROP COLUMN IF EXISTS domain_expires_at;

DROP TABLE IF EXISTS browser_scripts;
DROP TABLE IF EXISTS multistep_scripts;
DROP TABLE IF EXISTS heartbeats;

ALTER TABLE monitors DROP CONSTRAINT IF EXISTS monitors_type_check;
ALTER TABLE monitors
  ADD CONSTRAINT monitors_type_check
    CHECK (type IN ('http','tcp','dns','tls','keyword'));
