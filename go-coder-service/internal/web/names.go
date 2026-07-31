package web

import (
	"math/rand/v2"
	"strconv"
)

// Same pattern Coder uses: color-animal-0..99 (e.g. yellow-bird-23),
// mirroring frontend/src/lib/generateWorkspaceName.ts.

var nameColors = []string{
	"amber", "aqua", "azure", "beige", "black", "blue", "bronze", "coral",
	"crimson", "cyan", "emerald", "fuchsia", "gold", "gray", "green", "indigo",
	"ivory", "jade", "lavender", "lime", "magenta", "maroon", "olive", "orange",
	"pink", "plum", "purple", "red", "rose", "salmon", "scarlet", "silver",
	"tan", "teal", "turquoise", "violet", "white", "yellow",
}

var nameAnimals = []string{
	"ant", "badger", "bat", "bear", "beaver", "bee", "bird", "bison", "camel",
	"cat", "cheetah", "cobra", "condor", "crab", "crane", "deer", "dog",
	"dolphin", "donkey", "dove", "dragonfly", "duck", "eagle", "elephant",
	"elk", "falcon", "ferret", "finch", "fox", "frog", "gazelle", "gecko",
	"goat", "goose", "hawk", "hedgehog", "heron", "horse", "ibis", "jaguar",
	"koala", "lemur", "leopard", "lion", "lizard", "llama", "lynx", "marmot",
	"mole", "moose", "mouse", "newt", "otter", "owl", "panda", "panther",
	"parrot", "pelican", "penguin", "pony", "puma", "quail", "rabbit",
	"raccoon", "raven", "robin", "salmon", "seal", "shark", "sheep", "sloth",
	"snail", "sparrow", "squid", "stork", "swan", "tiger", "toad", "trout",
	"turtle", "walrus", "weasel", "whale", "wolf", "wombat", "yak", "zebra",
}

func GenerateWorkspaceName() string {
	color := nameColors[rand.IntN(len(nameColors))]
	animal := nameAnimals[rand.IntN(len(nameAnimals))]
	return color + "-" + animal + "-" + strconv.Itoa(rand.IntN(100))
}
