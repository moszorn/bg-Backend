package game

// Record 遊戲分數
type record struct {
	isDouble bool

	//Double 屬於哪一種類Double (只限DOUBLE, REDOUBLE, ZeroSuit表未設定)
	dbType CbSuit

	//身價方
	//TODO

	//遊戲最終叫品種類與線位 (C1 ~ Db7x2, BidYet 表未設定)
	contract CbBid
}
