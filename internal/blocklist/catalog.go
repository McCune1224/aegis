package blocklist

// CatalogEntry is a known blocklist an operator can enable without typing a URL.
type CatalogEntry struct {
	Name   string
	URL    string
	Format Format
}

// Catalog is a short list of well-known lists. Nothing here is fetched at build
// time; enabling one just stores its URL and format, and the fetcher downloads
// it on the next load.
var Catalog = []CatalogEntry{
	{
		Name:   "stevenblack",
		URL:    "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts",
		Format: FormatHosts,
	},
	{
		Name:   "adguard-dns",
		URL:    "https://adguardteam.github.io/HostlistsRegistry/assets/filter_1.txt",
		Format: FormatAdBlock,
	},
	{
		Name:   "oisd-basic",
		URL:    "https://big.oisd.nl/domainswild",
		Format: FormatDomains,
	},
}
