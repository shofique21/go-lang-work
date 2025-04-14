package main

import (
	"fmt"
)

var firstName = "Shofique"
var lastName = "Shahariar"

const USER = "Admin"
const (
	studentId = "CSE3432"
	result    = "3.60"
)

func main() {
	var fruit string = "Apple"
	profession := " Software Engineer"
	language := "GoLang"
	firstLng, secondLang, thirdLang := "C Programming", "Java Programming", "Python Programming"
	fmt.Println(fruit)
	fmt.Println("Hello " + firstName + " " + lastName)
	fmt.Println("Profession: " + profession + ", Programming language: " + language)
	fmt.Println("Other experience language: " + firstLng + ", " + secondLang + ", " + thirdLang)
	fmt.Println("User Type: " + USER)
	fmt.Println("Student Id: " + studentId + ", Result: " + result)
	isGolangPL := true
	isHtmlPL := false
	fmt.Println(isGolangPL)
	fmt.Println(isHtmlPL)
	var num1 int = 2000
	fmt.Println(num1)
	var num2 float64 = 500.65
	fmt.Println(num2)
	intro := `Hello,
	I am Shofique,
	Work as Software Engineer,
	Development langaues are Golang, Java, PHP.`
	fmt.Println(intro)
	message := "Welcome Golang"
	fmt.Printf("%c", message[5])
	fmt.Println(" ")
	vagetable := "Potato is most popular vagetable"
	fmt.Println(len(vagetable))

}
