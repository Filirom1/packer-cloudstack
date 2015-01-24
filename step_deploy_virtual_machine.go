package cloudstack

import (
	"fmt"
	"github.com/mindjiver/gopherstack"
	"github.com/mitchellh/multistep"
	"github.com/mitchellh/packer/common/uuid"
	"github.com/mitchellh/packer/packer"
)

type stepDeployVirtualMachine struct {
	id string
}

type bootCommandTemplateData struct {
	HTTPIP   string
	HTTPPort string
	Name     string
}

func (s *stepDeployVirtualMachine) Run(state multistep.StateBag) multistep.StepAction {
	client := state.Get("client").(*gopherstack.CloudstackClient)
	ui := state.Get("ui").(packer.Ui)
	c := state.Get("config").(config)
	sshKeyName := state.Get("ssh_key_name").(string)

	ui.Say("Creating virtual machine...")

	// Some random virtual machine name as it's temporary
	displayName := fmt.Sprintf("packer-%s", uuid.TimeOrderedUUID())

	// Massage any userData that we wish to send to the virtual
	// machine to help it boot properly.
	processTemplatedUserdata(state)
	userData := state.Get("user_data").(string)

	if c.ServiceOfferingId == "" && c.ServiceOffering != "" {
		serviceOfferingId, err := client.NameToId(c.ServiceOffering, "ServiceOffering", nil)
		if err != nil {
			err := fmt.Errorf("Error retrieveing service offering id: %s", err)
			state.Put("error", err)
			ui.Error(err.Error())
			return multistep.ActionHalt
		}
		c.ServiceOfferingId  = serviceOfferingId
	}


	if c.DiskOfferingId == "" && c.DiskOffering != "" {
		diskOfferingId, err := client.NameToId(c.DiskOffering, "DiskOffering", nil)
		if err != nil {
			err := fmt.Errorf("Error retrieving disk offering id: %s", err)
			state.Put("error", err)
			ui.Error(err.Error())
			return multistep.ActionHalt
		}
		c.DiskOfferingId = diskOfferingId
	}

	if c.ZoneId == "" && c.Zone != "" {
		params := map[string]string{
			"available": "true",
		}
		zoneId, err := client.NameToId(c.Zone, "Zone", params)
		if err != nil {
			err := fmt.Errorf("Error retrieving zone id: %s", err)
			state.Put("error", err)
			ui.Error(err.Error())
			return multistep.ActionHalt
		}
		c.ZoneId = zoneId
	}

	if c.TemplateId == "" && c.Template != "" {
		params := map[string]string{
			"zoneid": c.ZoneId,
			"templatefilter": "executable",
		}
		templateId, err := client.NameToId(c.Template, "Template", params)
		if err != nil {
			err := fmt.Errorf("Error retrieveing template id: %s", err)
			state.Put("error", err)
			ui.Error(err.Error())
			return multistep.ActionHalt
		}
		c.TemplateId = templateId
	}

	if len(c.NetworkIds) == 0 && len(c.Networks) > 0 {
		for i, network := range c.Networks {
			networkId, err := client.NameToId(network, "Network", nil)
			if err != nil {
				err := fmt.Errorf("Error retrieveing network id: %s", err)
				state.Put("error", err)
				ui.Error(err.Error())
				return multistep.ActionHalt
			}
			c.NetworkIds[i] = networkId
		}
	}

	// Create the virtual machine based on configuration
	response, err := client.DeployVirtualMachine(c.ServiceOfferingId,
		c.TemplateId, c.ZoneId, "", c.DiskOfferingId, displayName,
		c.NetworkIds, sshKeyName, "", userData, c.Hypervisor)

	if err != nil {
		err := fmt.Errorf("Error deploying virtual machine: %s", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	// Unpack the async jobid and wait for it
	vmid := response.Deployvirtualmachineresponse.ID
	jobid := response.Deployvirtualmachineresponse.Jobid
	client.WaitForAsyncJob(jobid, c.stateTimeout)

	// We use this in cleanup
	s.id = vmid

	// Store the virtual machine id for later use
	state.Put("virtual_machine_id", vmid)

	return multistep.ActionContinue
}

func (s *stepDeployVirtualMachine) Cleanup(state multistep.StateBag) {
	// If the virtual machine id isn't there, we probably never created it
	if s.id == "" {
		return
	}

	client := state.Get("client").(*gopherstack.CloudstackClient)
	ui := state.Get("ui").(packer.Ui)
	c := state.Get("config").(config)

	// Destroy the virtual machine we just created
	ui.Say("Destroying virtual machine...")

	response, err := client.DestroyVirtualMachine(s.id)
	if err != nil {
		ui.Error(fmt.Sprintf(
			"Error destroying virtual machine. Please destroy it manually."))
	}
	jobid := response.Destroyvirtualmachineresponse.Jobid
	client.WaitForAsyncJob(jobid, c.stateTimeout)
}

func processTemplatedUserdata(state multistep.StateBag) multistep.StepAction {
	ui := state.Get("ui").(packer.Ui)
	c := state.Get("config").(config)

	// If there is no userdata to process we just save back an
	// empty string.
	if c.UserData == "" {
		state.Put("user_data", "")
		return multistep.ActionContinue
	}

	httpIP := state.Get("http_ip").(string)
	httpPort := state.Get("http_port").(string)

	tplData := &bootCommandTemplateData{
		httpIP,
		httpPort,
		c.TemplateName,
	}

	userData, err := c.tpl.Process(c.UserData, tplData)
	if err != nil {
		err := fmt.Errorf("Error preparing boot command: %s", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	state.Put("user_data", userData)
	return multistep.ActionContinue
}
