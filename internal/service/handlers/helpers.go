package handlers

import "regexp"

func regexMap(r *regexp.Regexp, s string) map[string]string {

	args := r.FindStringSubmatch(s)
	if args == nil {
		return nil
	}

	names := r.SubexpNames()
	m := make(map[string]string, len(args))

	for i, v := range args {
		m[names[i]] = v
	}

	return m
}
