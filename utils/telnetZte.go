package utils

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/reiver/go-telnet"
)

// Function to connect to the OLT ZTE via telnet
func OltZteConnect(host string, port string, username string, password string) (*telnet.Conn, error) {
	// validar primero si se le llega al equipo por ping
	if err := PingHost(host, 3, 2); err != nil {
		return nil, err
	}

	address := fmt.Sprintf("%s:%s", host, port)
	conn, err := telnet.DialTo(address)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to OLT: %v", err)
	}

	// Create a context with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	//check connection if olt is asking for username
	if _, err = oltZteRead(conn, "Username", ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to OLT, expected string was 'Username' %v", err)
	}

	//send username and read response
	if _, err = OltZteSend(conn, username, "Password", 3000*time.Millisecond); err != nil {
		return nil, fmt.Errorf("error processing username: %v", err)
	}

	//send password and read response
	if _, err = OltZteSend(conn, password, "#", 3000*time.Millisecond); err != nil {
		return nil, fmt.Errorf("error processing password: %v", err)
	}

	return conn, nil
}

// Function to send string to the OLT via telnet
func OltZteSend(conn *telnet.Conn, command string, expect string, timeout time.Duration) (string, error) {
	//if debug mode is on, log every string send to the OLT
	if os.Getenv("GIN_MODE") == "debug" {
		Logline(command)
	}
	// Create a context with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	//crear canal para recibir la respuesta de DialContext
	errChan := make(chan error, 1)

	//crear go-routine and process
	go func() {
		_, err := conn.Write([]byte(command + "\r\n"))
		errChan <- err
		close(errChan)
	}()

	//wait for response on errChan and if error or timeout return err
	select {
	case <-ctx.Done():
		// Timeout occurred
		return "", fmt.Errorf("timeout occurred while sending string [%s] to %s: %w", command, conn.RemoteAddr(), ctx.Err())
	case err := <-errChan:
		if err != nil {
			return "", fmt.Errorf("error sending string [%s] to %s: %w", command, conn.RemoteAddr(), err)
		}
	}

	//if cmd was send without error check output for expected string
	response, err := oltZteRead(conn, expect, ctx)
	if err != nil {
		return response, fmt.Errorf("failed on read response, expected string was '%s': %w", expect, err)
	}

	if strings.Contains(response, "Error") || strings.Contains(response, "Invalid input") {
		return response, fmt.Errorf("OLT returns error sending string '%s', response was: [%s]", command, response)
	}

	return response, nil
}

// Function to read response from OLT via telnet
func oltZteRead(conn *telnet.Conn, expect string, ctx context.Context) (string, error) {
	//crear canal para recibir la respuesta de DialContext
	responseChan := make(chan string, 1)
	errChan := make(chan error, 1)

	//crear go-routine and process
	go func() {
		var out string
		var err error
		var buffer [1]byte
		recvData := buffer[:]
		var n int

		for {
			n, err = conn.Read(recvData)
			if err != nil {
				errChan <- err
				return
			}
			if n == 0 {
				errChan <- fmt.Errorf("no data received from [%s], connection may be closed", conn.RemoteAddr())
				return
			}
			// Append the read data to the output
			out += string(recvData)
			// Check if the `expect` string is in the output
			if strings.Contains(out, expect) {
				responseChan <- out
				return
			}
		}
	}()

	//wait for response on one of the two channels
	select {
	case <-ctx.Done():
		// Timeout occurred
		return "", fmt.Errorf("timeout on reading from %s - %w", conn.RemoteAddr(), ctx.Err())
	case err := <-errChan:
		// An error occurred
		return "", fmt.Errorf("error on reading from %s - %w", conn.RemoteAddr(), err)
	case response := <-responseChan:
		//if debug mode is on, log every output from the OLT
		if os.Getenv("GIN_MODE") == "debug" {
			Logline(response)
		}
		return response, nil
	}
}

// removeLastLine removes the last line from a string.
func OltRemoveLastLine(output string) string {

	// Split the output into lines
	lines := strings.Split(output, "\n")

	// If there are no lines, return the original output
	if len(lines) == 0 {
		return output
	}

	// Remove the last line
	lines = lines[:len(lines)-1]

	for key := range lines {
		// remove double spaces
		lines[key] = strings.ReplaceAll(lines[key], "  ", " ")

		// Remove \x0d and \x00 from the string
		lines[key] = strings.ReplaceAll(lines[key], "\x0d", "")
		lines[key] = strings.ReplaceAll(lines[key], "\x00", "")

		lines[key] = strings.ToLower(lines[key])
	}

	// Join the remaining lines back together
	return strings.Join(lines, "\n")
}

// Function to connect to the OLT VSOL via telnet
func OltVsolConnect(host string, port string, username string, password string) (*telnet.Conn, error) {
	// validar primero si se le llega al equipo por ping
	if err := PingHost(host, 3, 2); err != nil {
		return nil, err
	}

	address := fmt.Sprintf("%s:%s", host, port)
	conn, err := telnet.DialTo(address)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to OLT vsol: %v", err)
	}

	// Create a context with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	//check connection if olt is asking for username
	if _, err = oltZteRead(conn, "Login", ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to OLT, expected string was 'Login' %v", err)
	}

	//send username and read response
	if _, err = OltZteSend(conn, username, "Password", 2000*time.Millisecond); err != nil {
		return nil, fmt.Errorf("error processing username: %v", err)
	}

	//send password and read response
	if _, err = OltZteSend(conn, password, ">", 2000*time.Millisecond); err != nil {
		return nil, fmt.Errorf("error processing password: %v", err)
	}

	//send enable command and read response
	if _, err = OltZteSend(conn, "enable", "Password", 2000*time.Millisecond); err != nil {
		return nil, fmt.Errorf("error enabling cli on telnet session: %v", err)
	}

	//send password again and read response
	if _, err = OltZteSend(conn, password, "#", 2000*time.Millisecond); err != nil {
		return nil, fmt.Errorf("error processing password: %v", err)
	}

	//change to config mode
	if _, err = OltZteSend(conn, "conf t", "#", 2000*time.Millisecond); err != nil {
		return nil, fmt.Errorf("error changing to config mode: %v", err)
	}

	return conn, nil
}

// Function to connect to the OLT CDATA via telnet
func OltCdataConnect(host string, port string, username string, password string) (*telnet.Conn, error) {
	// validar primero si se le llega al equipo por ping
	if err := PingHost(host, 3, 2); err != nil {
		return nil, err
	}

	address := fmt.Sprintf("%s:%s", host, port)
	conn, err := telnet.DialTo(address)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to OLT vsol: %v", err)
	}

	// Create a context with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	//check connection if olt is asking for username
	if _, err = oltZteRead(conn, "name", ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to OLT, expected string was 'name' %v", err)
	}

	//send username and read response
	if _, err = OltZteSend(conn, username, "password", 2000*time.Millisecond); err != nil {
		return nil, fmt.Errorf("error processing username: %v", err)
	}

	//send password and read response
	if _, err = OltZteSend(conn, password, ">", 2000*time.Millisecond); err != nil {
		return nil, fmt.Errorf("error processing password: %v", err)
	}

	//send enable command and read response
	if _, err = OltZteSend(conn, "enable", "#", 2000*time.Millisecond); err != nil {
		return nil, fmt.Errorf("error enabling cli on telnet session: %v", err)
	}

	//change to config mode
	if _, err = OltZteSend(conn, "config", "#", 2000*time.Millisecond); err != nil {
		return nil, fmt.Errorf("error processing password: %v", err)
	}

	return conn, nil
}
