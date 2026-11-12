package web

// pageSize is the page_size setting, for a list's pager. An engine that
// reports none (a test double) gets the pager's own default.
func (s *Server) pageSize() int {
	return s.opts.Engine.Settings().PageSize
}
