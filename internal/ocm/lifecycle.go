package ocm

func (s *Service) Close() error { return s.lifecycle.Close() }
