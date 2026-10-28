package model3d

import "math"

// Small float32 vector and matrix types with System.Numerics' conventions:
// row vectors, so v' = v · M and a transform A then B is A · B.

type vec2 struct{ X, Y float32 }

type vec3 struct{ X, Y, Z float32 }

type vec4 struct{ X, Y, Z, W float32 }

func v3(x, y, z float32) vec3 { return vec3{x, y, z} }

func (a vec3) add(b vec3) vec3      { return vec3{a.X + b.X, a.Y + b.Y, a.Z + b.Z} }
func (a vec3) sub(b vec3) vec3      { return vec3{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }
func (a vec3) scale(s float32) vec3 { return vec3{a.X * s, a.Y * s, a.Z * s} }
func (a vec3) neg() vec3            { return vec3{-a.X, -a.Y, -a.Z} }
func (a vec3) dot(b vec3) float32   { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }
func (a vec3) cross(b vec3) vec3 {
	return vec3{a.Y*b.Z - a.Z*b.Y, a.Z*b.X - a.X*b.Z, a.X*b.Y - a.Y*b.X}
}
func (a vec3) lengthSquared() float32 { return a.dot(a) }
func (a vec3) length() float32        { return float32(math.Sqrt(float64(a.dot(a)))) }
func (a vec3) normalize() vec3 {
	l := a.length()
	return vec3{a.X / l, a.Y / l, a.Z / l}
}
func (a vec3) min(b vec3) vec3 { return vec3{min(a.X, b.X), min(a.Y, b.Y), min(a.Z, b.Z)} }
func (a vec3) max(b vec3) vec3 { return vec3{max(a.X, b.X), max(a.Y, b.Y), max(a.Z, b.Z)} }
func (a vec3) abs() vec3 {
	return vec3{float32(math.Abs(float64(a.X))), float32(math.Abs(float64(a.Y))), float32(math.Abs(float64(a.Z)))}
}
func (a vec3) finite() bool { return finite(a.X) && finite(a.Y) && finite(a.Z) }

// noNegativeZero turns -0 into 0, so the same point compares equal as a map key.
func (a vec3) noNegativeZero() vec3 { return vec3{a.X + 0, a.Y + 0, a.Z + 0} }

func clamp01(v float32) float32 { return max(0, min(1, v)) }

func (a vec4) clamp01() vec4 { return vec4{clamp01(a.X), clamp01(a.Y), clamp01(a.Z), clamp01(a.W)} }

func (a vec4) distance(b vec4) float32 {
	dx, dy, dz, dw := a.X-b.X, a.Y-b.Y, a.Z-b.Z, a.W-b.W
	return float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz + dw*dw)))
}

func finite(f float32) bool { return !math.IsNaN(float64(f)) && !math.IsInf(float64(f), 0) }

// mat4 is a 4×4 matrix, m[row][column].
type mat4 [4][4]float32

var identity = mat4{{1, 0, 0, 0}, {0, 1, 0, 0}, {0, 0, 1, 0}, {0, 0, 0, 1}}

func (a mat4) mul(b mat4) mat4 {
	var r mat4
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			r[i][j] = a[i][0]*b[0][j] + a[i][1]*b[1][j] + a[i][2]*b[2][j] + a[i][3]*b[3][j]
		}
	}
	return r
}

func scaleMatrix(x, y, z float32) mat4 {
	return mat4{{x, 0, 0, 0}, {0, y, 0, 0}, {0, 0, z, 0}, {0, 0, 0, 1}}
}

func uniformScale(s float32) mat4 { return scaleMatrix(s, s, s) }

func scaleVec(v vec3) mat4 { return scaleMatrix(v.X, v.Y, v.Z) }

func translation(v vec3) mat4 {
	m := identity
	m[3][0], m[3][1], m[3][2] = v.X, v.Y, v.Z
	return m
}

func sincos(radians float32) (float32, float32) {
	s, c := math.Sincos(float64(radians))
	return float32(s), float32(c)
}

func rotationX(r float32) mat4 {
	s, c := sincos(r)
	return mat4{{1, 0, 0, 0}, {0, c, s, 0}, {0, -s, c, 0}, {0, 0, 0, 1}}
}

func rotationY(r float32) mat4 {
	s, c := sincos(r)
	return mat4{{c, 0, -s, 0}, {0, 1, 0, 0}, {s, 0, c, 0}, {0, 0, 0, 1}}
}

func rotationZ(r float32) mat4 {
	s, c := sincos(r)
	return mat4{{c, s, 0, 0}, {-s, c, 0, 0}, {0, 0, 1, 0}, {0, 0, 0, 1}}
}

func (m mat4) isIdentity() bool { return m == identity }

func (m mat4) transform(v vec3) vec3 {
	return vec3{
		v.X*m[0][0] + v.Y*m[1][0] + v.Z*m[2][0] + m[3][0],
		v.X*m[0][1] + v.Y*m[1][1] + v.Z*m[2][1] + m[3][1],
		v.X*m[0][2] + v.Y*m[1][2] + v.Z*m[2][2] + m[3][2],
	}
}

func (m mat4) transformNormal(v vec3) vec3 {
	return vec3{
		v.X*m[0][0] + v.Y*m[1][0] + v.Z*m[2][0],
		v.X*m[0][1] + v.Y*m[1][1] + v.Z*m[2][1],
		v.X*m[0][2] + v.Y*m[1][2] + v.Z*m[2][2],
	}
}

func (m mat4) transpose() mat4 {
	var r mat4
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			r[i][j] = m[j][i]
		}
	}
	return r
}

func (m mat4) toF64() [16]float64 {
	var a [16]float64
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			a[i*4+j] = float64(m[i][j])
		}
	}
	return a
}

func det3(a, b, c, d, e, f, g, h, i float64) float64 {
	return a*(e*i-f*h) - b*(d*i-f*g) + c*(d*h-e*g)
}

// determinant by cofactor expansion along the first row.
func (m mat4) determinant() float32 {
	a := m.toF64()
	at := func(r, c int) float64 { return a[r*4+c] }
	var d float64
	for c := 0; c < 4; c++ {
		var cols [3]int
		k := 0
		for j := 0; j < 4; j++ {
			if j != c {
				cols[k] = j
				k++
			}
		}
		minor := det3(at(1, cols[0]), at(1, cols[1]), at(1, cols[2]),
			at(2, cols[0]), at(2, cols[1]), at(2, cols[2]),
			at(3, cols[0]), at(3, cols[1]), at(3, cols[2]))
		sign := 1.0
		if c%2 == 1 {
			sign = -1
		}
		d += sign * at(0, c) * minor
	}
	return float32(d)
}

// invert by Gauss-Jordan elimination; false when the matrix is singular.
func (m mat4) invert() (mat4, bool) {
	a := m.toF64()
	var inv [16]float64
	for i := 0; i < 4; i++ {
		inv[i*4+i] = 1
	}
	for col := 0; col < 4; col++ {
		pivot := col
		for r := col + 1; r < 4; r++ {
			if math.Abs(a[r*4+col]) > math.Abs(a[pivot*4+col]) {
				pivot = r
			}
		}
		if math.Abs(a[pivot*4+col]) < 1e-30 {
			return mat4{}, false
		}
		for j := 0; j < 4; j++ {
			a[col*4+j], a[pivot*4+j] = a[pivot*4+j], a[col*4+j]
			inv[col*4+j], inv[pivot*4+j] = inv[pivot*4+j], inv[col*4+j]
		}
		p := a[col*4+col]
		for j := 0; j < 4; j++ {
			a[col*4+j] /= p
			inv[col*4+j] /= p
		}
		for r := 0; r < 4; r++ {
			if r == col {
				continue
			}
			f := a[r*4+col]
			if f == 0 {
				continue
			}
			for j := 0; j < 4; j++ {
				a[r*4+j] -= f * a[col*4+j]
				inv[r*4+j] -= f * inv[col*4+j]
			}
		}
	}
	var r mat4
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			r[i][j] = float32(inv[i*4+j])
		}
	}
	return r, true
}

func pow32(x, y float32) float32 { return float32(math.Pow(float64(x), float64(y))) }
