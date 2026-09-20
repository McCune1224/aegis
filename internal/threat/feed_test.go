package threat

import (
	"testing"

	"github.com/stretchr/testify/require"

	"aegis/internal/store"
)

func TestParseFeedReadsDomainsAndTheirKinds(t *testing.T) {
	body := []byte(`# a comment
malware.example malware
phish.example phishing
bare.example

adjacent.example	extra words here
`)
	entries, err := ParseFeed(body)

	require.NoError(t, err)
	require.Len(t, entries, 4)
	require.Equal(t, "malware.example", entries[0].Domain.String())
	require.Equal(t, "malware", entries[0].Kind)
	require.Equal(t, "phishing", entries[1].Kind)
	require.Equal(t, DefaultKind, entries[2].Kind, "a bare domain means malware")
	require.Equal(t, "extra", entries[3].Kind, "the kind is the first field after the domain")
}

func TestParseFeedRejectsADomainTheDNSCannotCarry(t *testing.T) {
	_, err := ParseFeed([]byte("bad!.example malware\n"))

	require.Error(t, err)
	require.Contains(t, err.Error(), "line 1")
}

func TestIndexMatchesTheExactNameThenTheRegistrableDomain(t *testing.T) {
	index := BuildIndex(map[string]string{
		"malware.example": "malware",
		"evil.example":    "c2",
	})

	require.Equal(t, "malware", index.ThreatFor("malware.example"))
	require.Equal(t, "c2", index.ThreatFor("mail.evil.example"))
	require.Equal(t, "", index.ThreatFor("example.org"))
	require.Equal(t, "", index.ThreatFor("clean.example"))
	require.Equal(t, "", EmptyIndex.ThreatFor("malware.example"))
}

func TestServicePublishesAFeedIndexForTheLog(t *testing.T) {
	database, err := store.Open(t.Context(), t.TempDir()+"/aegis.db")
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	service := NewService(database, nil)
	require.Equal(t, "", service.ThreatFor("malware.example"))

	service.PublishIndex(map[string]string{"malware.example": "malware"})
	require.Equal(t, "malware", service.ThreatFor("malware.example"))
	require.Equal(t, "malware", service.ThreatFor("mail.malware.example"))
	require.NoError(t, service.Close())
}
