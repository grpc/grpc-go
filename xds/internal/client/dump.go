/*
 *
 * Copyright 2021 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package client

// DumpLDS returns the status and contents of LDS.
func (c *clientImpl) DumpLDS() (version string, _ map[string]LDSUpdateWithMD) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ret := make(map[string]LDSUpdateWithMD, len(c.ldsMD))
	for s, md := range c.ldsMD {
		ret[s] = LDSUpdateWithMD{
			MD:  md,
			Raw: c.ldsCache[s].Raw,
		}
	}
	return c.ldsVersion, ret
}

// DumpRDS returns the status and contents of RDS.
func (c *clientImpl) DumpRDS() (version string, _ map[string]RDSUpdateWithMD) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ret := make(map[string]RDSUpdateWithMD, len(c.rdsMD))
	for s, md := range c.rdsMD {
		ret[s] = RDSUpdateWithMD{
			MD:  md,
			Raw: c.rdsCache[s].Raw,
		}
	}
	return c.rdsVersion, ret
}

// DumpCDS returns the status and contents of CDS.
func (c *clientImpl) DumpCDS() (version string, _ map[string]CDSUpdateWithMD) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ret := make(map[string]CDSUpdateWithMD, len(c.cdsMD))
	for s, md := range c.cdsMD {
		ret[s] = CDSUpdateWithMD{
			MD:  md,
			Raw: c.cdsCache[s].Raw,
		}
	}
	return c.cdsVersion, ret
}

// DumpEDS returns the status and contents of EDS.
func (c *clientImpl) DumpEDS() (version string, _ map[string]EDSUpdateWithMD) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ret := make(map[string]EDSUpdateWithMD, len(c.edsMD))
	for s, md := range c.edsMD {
		ret[s] = EDSUpdateWithMD{
			MD:  md,
			Raw: c.edsCache[s].Raw,
		}
	}
	return c.edsVersion, ret
}
