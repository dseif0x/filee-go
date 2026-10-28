package dxf

// valueKind is the type of the value of a group code (GroupCodeValueType).
type valueKind uint8

const (
	kindNone valueKind = iota
	kindString
	kindDouble
	kindInt16
	kindInt32
	kindInt64
	kindHandle
	kindBool
	kindChunk
)

// kindOf classifies a group code like ACadSharp's GroupCodeValue.TransformValue
// (Byte codes 280-289 are 16-bit integers on disk, as there).
func kindOf(code int) valueKind {
	switch {
	case code >= 0 && code <= 4:
		return kindString
	case code == 5:
		return kindHandle
	case code >= 6 && code <= 9:
		return kindString
	case code >= 10 && code <= 39:
		return kindDouble
	case code >= 40 && code <= 59:
		return kindDouble
	case code >= 60 && code <= 79:
		return kindInt16
	case code >= 90 && code <= 99:
		return kindInt32
	case code >= 100 && code <= 102:
		return kindString
	case code == 105:
		return kindHandle
	case code >= 110 && code <= 149:
		return kindDouble
	case code >= 160 && code <= 169:
		return kindInt64
	case code >= 170 && code <= 179:
		return kindInt16
	case code >= 210 && code <= 239:
		return kindDouble
	case code >= 270 && code <= 289:
		return kindInt16
	case code >= 290 && code <= 299:
		return kindBool
	case code >= 300 && code <= 309:
		return kindString
	case code >= 310 && code <= 319:
		return kindChunk
	case code >= 320 && code <= 369:
		return kindHandle
	case code >= 370 && code <= 389:
		return kindInt16
	case code >= 390 && code <= 399:
		return kindHandle
	case code >= 400 && code <= 409:
		return kindInt16
	case code >= 410 && code <= 419:
		return kindString
	case code >= 420 && code <= 429:
		return kindInt32
	case code >= 430 && code <= 439:
		return kindString
	case code >= 440 && code <= 459:
		return kindInt32
	case code >= 460 && code <= 469:
		return kindDouble
	case code >= 470 && code <= 479:
		return kindString
	case code >= 480 && code <= 481:
		return kindHandle
	case code == 999:
		return kindString
	case code >= 1000 && code <= 1003:
		return kindString
	case code == 1004:
		return kindChunk
	case code >= 1005 && code <= 1009:
		return kindHandle
	case code >= 1010 && code <= 1059:
		return kindDouble
	case code >= 1060 && code <= 1070:
		return kindInt16
	case code == 1071:
		return kindInt32
	}
	return kindNone
}

// Section and structure tokens.
const (
	tokSection     = "SECTION"
	tokEndSection  = "ENDSEC"
	tokEOF         = "EOF"
	tokTable       = "TABLE"
	tokEndTable    = "ENDTAB"
	tokHeader      = "HEADER"
	tokClasses     = "CLASSES"
	tokClass       = "CLASS"
	tokTables      = "TABLES"
	tokBlocks      = "BLOCKS"
	tokEntities    = "ENTITIES"
	tokObjects     = "OBJECTS"
	tokAcdsData    = "ACDSDATA"
	tokThumbnail   = "THUMBNAILIMAGE"
	binarySentinel = "AutoCAD Binary DXF\r\n\x1a\x00"
)
