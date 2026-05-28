import os
from fpdf import FPDF
from PIL import Image

class RateRudderPDF(FPDF):
    def __init__(self):
        # Initialize with Letter size, portrait mode, millimeters
        super().__init__(orientation="P", unit="mm", format="Letter")
        self.set_margins(left=20, top=20, right=20)
        self.set_auto_page_break(auto=True, margin=20)
        
    def header(self):
        if self.page_no() == 1:
            return  # Suppress header on cover page
            
        # Top banner line
        self.set_draw_color(30, 41, 59) # Slate Accent
        self.set_line_width(0.5)
        self.line(20, 15, 195.9, 15)
        
        # Header text
        self.set_font("helvetica", "I", 8)
        self.set_text_color(100, 116, 139) # Muted slate
        self.cell(w=0, h=5, text="RateRudder - Step-by-Step User Guide", align="R", new_x="LMARGIN", new_y="NEXT")
        self.ln(5)

    def footer(self):
        if self.page_no() == 1:
            return  # Suppress footer on cover page
            
        # Bottom divider line
        self.set_draw_color(226, 232, 240) # Light slate
        self.set_line_width(0.3)
        self.line(20, 264.4, 195.9, 264.4)
        
        self.set_y(-15)
        self.set_font("helvetica", "", 8)
        self.set_text_color(148, 163, 184) # Muted light slate
        
        # Left side info
        self.cell(w=90, h=10, text="Smart Energy Optimization Guide", align="L")
        # Right side page number
        self.cell(w=0, h=10, text=f"Page {self.page_no()}", align="R")

    def create_cover_page(self):
        self.add_page()
        
        # Background design - Dark elegant slate top band
        self.set_fill_color(15, 23, 42) # Slate Dark
        self.rect(0, 0, 215.9, 110, "F")
        
        # Top accent line - Orange/Amber
        self.set_fill_color(245, 158, 11) # Amber Accent
        self.rect(0, 110, 215.9, 4, "F")
        
        # Title inside the dark band (just "RateRudder System User Guide" as requested)
        self.set_y(45)
        self.set_font("helvetica", "B", 26)
        self.set_text_color(255, 255, 255)
        self.cell(w=0, h=12, text="RateRudder System User Guide", align="C", new_x="LMARGIN", new_y="NEXT")
        
        # Main content area - Subtitle & details
        self.set_y(135)
        self.set_font("helvetica", "B", 16)
        self.set_text_color(30, 41, 59)
        self.cell(w=0, h=8, text="Quick Start & Step-by-Step Walkthrough", align="C", new_x="LMARGIN", new_y="NEXT")
        
        self.ln(10)
        
        # Description
        self.set_font("helvetica", "", 11)
        self.set_text_color(71, 85, 105)
        intro_text = (
            "This guide walks you through setting up and using the RateRudder optimization system. "
            "Learn how to create your site, connect your devices, customize grid restrictions, configure location "
            "parameters, monitor real-time savings automation, and read the 24-hour simulation forecasts."
        )
        self.multi_cell(w=160, h=6, text=intro_text, align="C")

    def add_step_page(self, step_num, title, description, image_path, note_type="info", note_text=None):
        self.add_page()
        
        # Step header
        self.set_font("helvetica", "B", 14)
        self.set_text_color(15, 23, 42) # Slate Dark
        self.cell(w=0, h=8, text=f"Step {step_num}: {title}", new_x="LMARGIN", new_y="NEXT")
        
        # Accent bar under heading
        self.set_fill_color(37, 99, 235) # Primary Blue
        self.rect(20, self.get_y() + 1, 30, 1.5, "F")
        self.ln(6)
        
        # Description text
        self.set_font("helvetica", "", 10)
        self.set_text_color(71, 85, 105) # Charcoal
        self.multi_cell(w=0, h=5.5, text=description, new_x="LMARGIN", new_y="NEXT")
        self.ln(4)
        
        # Load and scale image to fit page beautifully
        if os.path.exists(image_path):
            img = Image.open(image_path)
            img_w, img_h = img.size
            aspect = img_h / img_w
            
            # Default width: 165mm
            w_to_use = 165
            h_to_use = w_to_use * aspect
            
            # If the image is too tall, scale down the height to keep it on one page
            max_allowed_h = 105.0
            if h_to_use > max_allowed_h:
                h_to_use = max_allowed_h
                w_to_use = h_to_use / aspect
                
            x_pos = 20 + (175.9 - w_to_use) / 2
            y_pos = self.get_y()
            
            # Add image border/shadow effect
            self.set_draw_color(226, 232, 240)
            self.set_fill_color(255, 255, 255)
            self.rect(x_pos - 1, y_pos - 1, w_to_use + 2, h_to_use + 2, "D")
            
            self.image(image_path, x=x_pos, y=y_pos, w=w_to_use, h=h_to_use)
            self.set_y(y_pos + h_to_use + 6)
        else:
            self.set_font("helvetica", "B", 10)
            self.set_text_color(220, 38, 38)
            self.cell(w=0, h=8, text=f"[Image missing: {image_path}]", new_x="LMARGIN", new_y="NEXT")
            self.ln(4)
            
        # Notes block / Callout
        if note_text:
            # Set colors based on note type
            if note_type == "warning":
                bg_color = (255, 251, 235)     # Light Amber
                border_color = (245, 158, 11)  # Amber
                text_color = (180, 83, 9)      # Dark Amber
                label = "IMPORTANT:"
            elif note_type == "success":
                bg_color = (240, 253, 250)     # Light Emerald
                border_color = (16, 185, 129)  # Emerald
                text_color = (6, 95, 70)       # Dark Emerald
                label = "PRO-TIP:"
            else: # info
                bg_color = (240, 249, 255)     # Light Sky Blue
                border_color = (56, 189, 248)  # Sky Blue
                text_color = (7, 89, 133)      # Dark Sky Blue
                label = "NOTE:"
                
            y_note_start = self.get_y()
            
            # Estimate text height using a slightly narrower printable width to account for the margin indent (w=168mm)
            self.set_font("helvetica", "", 9.5)
            lines_h = self.multi_cell(w=168, h=5, text=f"{label} {note_text}", dry_run=True, output="HEIGHT")
            box_h = lines_h + 6
            
            # Draw Callout Background
            self.set_fill_color(*bg_color)
            self.rect(20, y_note_start, 175.9, box_h, "F")
            
            # Draw left vertical accent line
            self.set_fill_color(*border_color)
            self.rect(20, y_note_start, 2.5, box_h, "F")
            
            # Print Callout Text
            # We temporarily set the left margin to 25 to ensure wrapped lines align perfectly and do not clip the accent line
            self.set_left_margin(25)
            self.set_y(y_note_start + 3)
            self.set_x(25)
            self.set_text_color(*text_color)
            
            # Bold Label, normal body text
            self.set_font("helvetica", "B", 9.5)
            self.write(5, f"{label} ")
            self.set_font("helvetica", "", 9.5)
            self.write(5, note_text)
            self.ln(box_h - 3)
            
            # Restore standard page margins
            self.set_left_margin(20)

def main():
    pdf = RateRudderPDF()
    pdf.create_cover_page()
    
    # Step 1
    pdf.add_step_page(
        step_num=1,
        title="Creating a New Site",
        description=(
            "To begin using the RateRudder optimizer, your first step is to initialize a Site. "
            "A Site serves as a workspace representing your physical installation of solar panels, "
            "batteries, and meter controllers. By monitoring and optimizing each site independently, "
            "the system delivers personalized charging and discharging schedules based on localized factors."
        ),
        image_path="image.png",
        note_type="info",
        note_text=(
            "A site can also be simplified as a residence. In the rare case your residence "
            "contains multiple battery controllers or solar arrays, you might need to create multiple "
            "distinct sites to manage them independently."
        )
    )
    
    # Step 2
    pdf.add_step_page(
        step_num=2,
        title="Initial Dashboard & Setup Warnings",
        description=(
            "When a site is first created, you will be taken to the main dashboard. Because no hardware "
            "or pricing configurations have been established, you will see critical warning banners at the "
            "top of the page. These banners indicate that the system cannot automate your energy storage "
            "until setup is completed."
        ),
        image_path="image copy.png",
        note_type="warning",
        note_text=(
            "Before anything will work, you will need to configure your battery system and your "
            "utility provider information. Follow the links in the warning banners or navigate to "
            "the Settings tab to configure these requirements."
        )
    )
    
    # Step 3
    pdf.add_step_page(
        step_num=3,
        title="Utility & Energy Storage System Configuration",
        description=(
            "Navigate to the device configuration section in Settings to connect your household components. "
            "First, select your electricity Utility Service to enable real-time price tracking. Next, select "
            "your Energy Storage System (ESS) type to establish hardware communication. Once selected, they "
            "will display a 'PENDING SAVE' badge until you commit the changes."
        ),
        image_path="image copy 2.png",
        note_type="info",
        note_text=None  # Removed note as requested
    )
    
    # Step 4
    pdf.add_step_page(
        step_num=4,
        title="Grid Restrictions Settings",
        description=(
            "The Grid Restrictions card governs how your battery and solar system interact with the local "
            "utility grid. Using the toggle switches, you can dictate permissions such as allowing the grid "
            "to charge the battery (useful for off-peak rate charging) and allowing solar energy or battery "
            "discharges to be exported back to the grid for credits."
        ),
        image_path="image copy 3.png",
        note_type="warning",
        note_text=(
            "Make sure to choose the grid restrictions that are applicable to your installation. Selecting "
            "unsupported behaviors can lead to billing errors or compliance issues with your utility provider."
        )
    )
    
    # Step 5
    pdf.add_step_page(
        step_num=5,
        title="Location Settings",
        description=(
            "Accurate location coordinates are critical for solar generation predictions. By specifying your "
            "Country and Zip/Postal Code, the optimizer retrieves regional weather forecasts. Providing "
            "your Roof Solar Panel Direction (e.g., South, West) allows the system to model the sun's angle "
            "and calculate expected hourly solar generation with high precision."
        ),
        image_path="image copy 4.png",
        note_type="success",
        note_text=(
            "A south-facing solar panel direction is typical in the Northern Hemisphere for maximum total yield, "
            "but make sure to enter your actual physical panel orientation to keep predictions accurate."
        )
    )
    
    # Step 6
    pdf.add_step_page(
        step_num=6,
        title="Dashboard & Active Automation Monitoring",
        description=(
            "Once configuration is saved, the dashboard begins displaying active energy data and savings statistics. "
            "The top banner tracks your 'Savings Today' (divided into solar and battery contributions), home usage, "
            "solar generation, battery usage, and net grid costs/credits. Below, the system logs a chronological "
            "timeline showing exactly when and why the optimizer chose automated actions (e.g. discharging at high rates)."
        ),
        image_path="image copy 5.png",
        note_type="success",
        note_text=(
            "The timeline events provide full transparency. For example, you can see the exact time the battery "
            "reached reserve capacity or when the system triggered a discharge to minimize grid imports during peak price hours."
        )
    )
    
    # Step 7
    pdf.add_step_page(
        step_num=7,
        title="24-Hour Simulation & Forecasting",
        description=(
            "The 24-Hour Simulation charts project your energy state over the next day assuming no manual overrides. "
            "The top graph details the battery state of charge (%) relative to your Reserve threshold (e.g., 25%). "
            "The bottom graph maps the predicted solar generation (kWh) hour-by-hour based on weather forecasts and "
            "location parameters, allowing you to anticipate battery charging cycles."
        ),
        image_path="image copy 6.png",
        note_type="info",
        note_text=(
            "The forecast page shows an estimated look at the next 24 hours. Because it relies on predictive "
            "models for weather and household consumption, actual battery levels may vary slightly as the day unfolds."
        )
    )
    
    # Output the PDF to the web public folder
    script_dir = os.path.dirname(os.path.abspath(__file__))
    project_root = os.path.dirname(script_dir)
    output_dir = os.path.join(project_root, "web", "public")
    os.makedirs(output_dir, exist_ok=True)
    
    output_filepath = os.path.join(output_dir, "RateRudder_User_Guide.pdf")
    pdf.output(output_filepath)
    print(f"User guide PDF generated successfully as: {os.path.abspath(output_filepath)}")

if __name__ == "__main__":
    main()
