// SPDX-License-Identifier: MIT

package main

// capabilities is the capability document (docs/CAPABILITIES.md,
// "Features of the IEC 102 images"). Every true feature is exercised by the
// smoke test against the image's own server and by the cross-stack run.
func capabilities() map[string]any {
	features := map[string]bool{}
	for _, f := range []string{
		"generalInterrogation", "groupInterrogation", "counterInterrogation", "read", "testCommand",
		"directCommands", "selectBeforeOperate", "commandDeactivation", "timeTaggedCommands", "spontaneousOnCommand",
		"apciParameters", "multipleConnections", "pointQualityOnBitstring", "tls", "tlsServer", "tlsClient",
		"dataTransferEvents", "fileTransfer", "fileServer", "fileClient", "fileUploadServer", "fileUploadClient",
		"fileDirectoryServer", "fileDirectoryClient", "redundancyServer", "redundancyClient", "secureAuthentication",
		"balancedMode", "distinctBalancedAddresses",
	} {
		features[f] = false
	}
	for _, f := range []string{
		"unbalancedMode", "clockSync", "integratedTotals", "readTime", "setTime",
		"productIdentification", "tariffInformation", "reeSession",
	} {
		features[f] = true
	}
	return map[string]any{
		"schemaVersion":  schemaVersion,
		"adapter":        adapterName,
		"adapterVersion": adapterVersion,
		"protocol":       standardName,
		"standard":       standardName,
		"transport":      "tcp-stream",
		"linkModes":      []string{"unbalanced"},
		"upstream": map[string]any{
			"name":              "otfabric/go-iec60870-5",
			"version":           upstreamVersion,
			"revision":          upstreamRevision,
			"license":           "MIT",
			"language":          "Go",
			"independentEngine": false,
		},
		"roles":        map[string]bool{"server": true, "client": true},
		"features":     features,
		"pointTypes":   []string{},
		"commandTypes": []string{},
		// monitor: the types whose objects the client reports (and the
		// server sends 8, 11, 71, 72, 135, 136 of them); control: the
		// requests the client sends and the server answers.
		"asduTypes": map[string][]int{
			"monitor": {2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 71, 72, 135, 136},
			"control": {100, 103, 122, 123, 133, 134, 181, 183, 187},
		},
		"linkAddressSizes": []int{1, 2},
		"caSizes":          []int{1, 2},
		"profiles":         []string{profileStandard, profileREE},
		"clientOperations": operations,
	}
}
