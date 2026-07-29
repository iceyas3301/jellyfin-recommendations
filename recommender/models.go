package recommender

type UserCollectionInfo struct {
	CollectionID string
	ItemIDs      map[string]bool
}

type user struct {
	ID   string `json:"Id"`
	Name string `json:"Name"`
}

type baseItem struct {
	ID   string `json:"Id"`
	Name string `json:"Name"`
}

type queryUserFavoritesResponse struct {
	Items []baseItem `json:"Items"`
}

type existingCollectionsResponse struct {
	Items []struct {
		ID   string `json:"Id"`
		Name string `json:"Name"`
	} `json:"Items"`
}

type createCollectionResponse struct {
	ID string `json:"Id"`
}
