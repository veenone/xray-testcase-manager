ifneq (,$(wildcard ./.env))
    include .env
    export
endif

ifeq ($(origin DASC_TOOLKIT), undefined)
    $(info > Local tool definition not found. using environment variable...)    
    include $(DASC_TOOLKIT)\tool\Makefile_include
else
    $(info > Local tool definition found. proceed with the process...)
endif

$(info > Running DASC Toolkit...)
include $(DASC_TOOLKIT)\tool\Makefile_include
