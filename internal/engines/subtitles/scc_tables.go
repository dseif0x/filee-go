package subtitles

// Special characters (0x11 0x30–0x3F); 0x39 is a transparent space.
var sccSpecial = []rune("®°½¿™¢£♪à èâêîôû")

// Extended characters, Spanish / French / miscellaneous (0x12 0x20–0x3F).
var sccExtended1 = []rune("ÁÉÓÚÜü‘¡*’—©℠•“”ÀÂÇÈÊËëÎÏïÔÙùÛ«»")

// Extended characters, Portuguese / German / Danish (0x13 0x20–0x3F).
var sccExtended2 = []rune("ÃãÍÌìÒòÕõ{}\\^_|~ÄäÖöß¥¤│ÅåØø┌┐└┘")
