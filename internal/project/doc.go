// Package project finds and parses the .vibe files of a project.
//
// A project is every .vibe file in the project folder and its subfolders,
// compiled together. The go/ folder of the project, the .vibe/ build
// folder and hidden folders are not scanned. Hidden files are skipped too,
// so editor lock and backup files never join the program.
package project
