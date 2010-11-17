run: clean unfollow 
	./unfollow
 
include $(GOROOT)/src/Make.inc

TARG=unfollow
GOFILES=\
    main.go\
    settings.go\

include $(GOROOT)/src/Make.cmd
