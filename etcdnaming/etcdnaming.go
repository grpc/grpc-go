package etcdnaming

import (
	"io/ioutil"
	"net/http"
	"strings"
)

func parser(body string) map[string]string {
	if body == "" {
		return nil
	}
	res := make(map[string]string)
	str := strings.Replace(body, "nodes", "Nodes", -1)
	subStr := strings.Split(str, "node")
	for _, entry := range subStr {
		i := 0
		var last int
		if strings.Contains(entry, "prevNode") {
			last = strings.IndexAny(entry, "prevNode")
		} else {
			last = len(entry)
		}
		for i < last {
			if entry[i] == '{' {
				j := i + 1
				subEntry := []byte{}
				for j < len(entry) && entry[j] != '{' && entry[j] != '}' {
					subEntry = append(subEntry, entry[j])
					j++
				}
				if j < len(entry) && entry[j] == '}' {
					var (
						key   string
						value string
					)
					for _, val := range strings.Split(string(subEntry), ",") {
						if strings.Contains(val, "key") {
							key = strings.Replace(strings.SplitN(val, ":", 2)[1], `"`, "", -1)
						}
						if strings.Contains(val, "value") {
							value = strings.Replace(strings.SplitN(val, ":", 2)[1], `"`, "", -1)
						}
						res[key] = value

					}
				}
				i = j
			} else {
				i++
			}
		}
	}
	return res
}

func Etcdnaming(host string, key string, watch bool) map[string]string {
	var comm string
	if !watch {
		comm = "http://" + host + "/v2/keys/" + key + "/?recursive=true"
	} else {
		comm = "http://" + host + "/v2/keys/" + key + `?wait=true&recursive=true`
	}
	resp, err := http.Get(comm)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	var body []byte
	body, err = ioutil.ReadAll(resp.Body)
	res := parser(string(body))
	return res
}
