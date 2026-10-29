// Package shrink minimizes a recorded choice sequence: it deletes and zeroes
// blocks of draws and lowers values, keeping a change only when the same
// failure signature (detector, invariant) still fires.
package shrink
