package nethernet

import "regexp"

// candidateRe matches the connection address in an ICE candidate, e.g.
// "candidate:123 1 udp 2122260223 172.17.0.2 19132 typ host ...".
var candidateRe = regexp.MustCompile(`(candidate:\S+ \d+ (?i:udp|tcp) \d+ )(\S+)( \d+ typ )`)

// RewriteCandidates replaces the address of every ICE candidate found in s
// (a bare candidate or a full SDP) with ip. It returns the new string and the
// number of candidates changed.
func RewriteCandidates(s, ip string) (string, int) {
	n := 0
	out := candidateRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := candidateRe.FindStringSubmatch(m)
		if sub[2] == ip {
			return m
		}
		n++
		return sub[1] + ip + sub[3]
	})
	return out, n
}
