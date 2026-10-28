package model3d

// Splits a polygon (OBJ, PLY and FBX faces with more than three corners) into
// triangles by ear clipping in the plane of the polygon, so concave faces come
// out right; a fan is the fallback for faces that are not simple polygons.

func addPolygon(p *Part, corners []int) {
	switch {
	case len(corners) < 3:
		return
	case len(corners) == 3:
		p.AddTriangle(corners[0], corners[1], corners[2])
		return
	}
	if len(corners) > 64 || !earClip(p, corners) {
		for i := 1; i+1 < len(corners); i++ {
			p.AddTriangle(corners[0], corners[i], corners[i+1])
		}
	}
}

func earClip(p *Part, corners []int) bool {
	n := len(corners)
	// Newell's normal gives the polygon's plane and winding even for concave polygons.
	var normal vec3
	for i := 0; i < n; i++ {
		a := p.Position(corners[i])
		b := p.Position(corners[(i+1)%n])
		normal = normal.add(vec3{(a.Y - b.Y) * (a.Z + b.Z), (a.Z - b.Z) * (a.X + b.X), (a.X - b.X) * (a.Y + b.Y)})
	}
	if normal.lengthSquared() == 0 || !finite(normal.X) {
		return false
	}
	// Project onto the plane: drop the dominant axis, keeping the polygon counter-clockwise.
	abs := normal.abs()
	points := make([]vec2, n)
	for i := 0; i < n; i++ {
		q := p.Position(corners[i])
		switch {
		case abs.X >= abs.Y && abs.X >= abs.Z:
			if normal.X >= 0 {
				points[i] = vec2{q.Y, q.Z}
			} else {
				points[i] = vec2{q.Z, q.Y}
			}
		case abs.Y >= abs.Z:
			if normal.Y >= 0 {
				points[i] = vec2{q.Z, q.X}
			} else {
				points[i] = vec2{q.X, q.Z}
			}
		default:
			if normal.Z >= 0 {
				points[i] = vec2{q.X, q.Y}
			} else {
				points[i] = vec2{q.Y, q.X}
			}
		}
	}

	remaining := make([]int, n)
	for i := range remaining {
		remaining[i] = i
	}
	count := n
	guard := n * n
	start := len(p.Indices)
	for count > 3 && guard > 0 {
		guard--
		clipped := false
		for i := 0; i < count; i++ {
			prev := remaining[(i+count-1)%count]
			cur := remaining[i]
			next := remaining[(i+1)%count]
			if cross2(points[prev], points[cur], points[next]) <= 0 {
				continue // reflex or degenerate corner
			}
			inside := false
			for j := 0; j < count && !inside; j++ {
				k := remaining[j]
				if k != prev && k != cur && k != next && inTriangle(points[k], points[prev], points[cur], points[next]) {
					inside = true
				}
			}
			if inside {
				continue
			}
			p.AddTriangle(corners[prev], corners[cur], corners[next])
			copy(remaining[i:count], remaining[i+1:count])
			count--
			clipped = true
			break
		}
		if !clipped {
			p.Indices = p.Indices[:start]
			return false
		}
	}
	p.AddTriangle(corners[remaining[0]], corners[remaining[1]], corners[remaining[2]])
	return true
}

func cross2(a, b, c vec2) float32 { return (b.X-a.X)*(c.Y-a.Y) - (b.Y-a.Y)*(c.X-a.X) }

func inTriangle(q, a, b, c vec2) bool {
	return cross2(a, b, q) >= 0 && cross2(b, c, q) >= 0 && cross2(c, a, q) >= 0
}
