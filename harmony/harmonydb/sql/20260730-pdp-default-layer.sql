-- Ship the recommended PDP configuration as a pre-built layer so operators can
-- `curio run --layers=gui,pdp` without hand-building it. Values match the guide at
-- https://docs.curiostorage.org/experimental-features/enable-pdp
--
-- HTTP.DomainName is deliberately left commented out: it must be a public DNS name
-- whose A record points at this node so Let's Encrypt can issue a certificate, so
-- there is no value we can ship that works. Operators must uncomment and set it
-- before running with this layer.
INSERT INTO harmony_config (title, config) VALUES
  ('pdp', '
  [Subsystems]
  EnableParkPiece = true
  EnablePDP = true
  EnableCommP = true
  EnableMoveStorage = true

  [HTTP]
  Enable = true
  #DomainName = "pdp.mydomain.com"
  ListenAddress = "0.0.0.0:443"
  ')
  ON CONFLICT (title) DO NOTHING; -- SPs may have a pdp layer defined already.
