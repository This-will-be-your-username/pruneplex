package radarr

import "testing"

func TestSuggestRanksLikelyMatchesFirst(t *testing.T) {
	movies := []Movie{
		{ID: 1, Title: "Alien", Year: 1979},
		{ID: 2, Title: "Aliens", Year: 1986},
		{ID: 3, Title: "The Lord of the Rings: The Fellowship of the Ring", Year: 2001},
		{ID: 4, Title: "Casablanca", Year: 1942},
	}
	got := Suggest(Identity{Title: "Lord of the Rings Fellowship of the Ring", Year: 2001}, movies, 5)
	if len(got) == 0 || got[0].ID != 3 {
		t.Fatalf("expected Lord of the Rings first, got %+v", got)
	}
	for _, m := range got {
		if m.ID == 4 {
			t.Fatalf("unrelated movie suggested: %+v", got)
		}
	}

	got = Suggest(Identity{Title: "Alien", Year: 1986}, movies, 5)
	if len(got) < 2 || got[0].ID != 2 {
		t.Fatalf("year should break the Alien/Aliens tie toward Aliens (1986), got %+v", got)
	}
	if len(Suggest(Identity{Title: "Alien"}, movies, 1)) != 1 {
		t.Fatal("limit not applied")
	}
	if Suggest(Identity{Title: ""}, movies, 5) != nil {
		t.Fatal("empty title should produce no suggestions")
	}
}
